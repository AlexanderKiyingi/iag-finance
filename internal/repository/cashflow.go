package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// CashFlowRow is the net cash movement attributable to an activity category.
type CashFlowRow struct {
	Category string `json:"category"` // operating | investing | financing
	NetCash  string `json:"netCash"`  // base currency; positive = inflow
}

// CashFlow summarises cash movement over [from, to] for the entity scope,
// classified into operating / investing / financing activities. For every posted
// entry that touches the Cash account (1000), the cash delta equals the net of
// the entry's non-cash legs; summing those legs by category attributes the cash
// flow without needing opening balances. The categories sum to the net change in
// cash for the period.
func (r *Repository) CashFlow(ctx context.Context, from, to *time.Time, entityIDs []uuid.UUID) ([]CashFlowRow, error) {
	rows, err := r.pool.Query(ctx, `
		WITH cash_entries AS (
			SELECT DISTINCT jl.journal_entry_id
			FROM journal_lines jl JOIN chart_of_accounts coa ON coa.id = jl.account_id
			WHERE coa.code = '1000'
		)
		SELECT
			CASE
				WHEN coa.code IN ('1500', '1510') THEN 'investing'
				WHEN coa.account_type = 'equity' THEN 'financing'
				ELSE 'operating'
			END AS category,
			SUM(jl.credit_base - jl.debit_base)::text AS net_cash
		FROM journal_lines jl
		JOIN chart_of_accounts coa ON coa.id = jl.account_id
		JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
			AND ($1::date IS NULL OR je.accounting_date >= $1)
			AND ($2::date IS NULL OR je.accounting_date <= $2)
			AND je.entity_id = ANY($3::uuid[])
		WHERE jl.journal_entry_id IN (SELECT journal_entry_id FROM cash_entries)
		  AND coa.code <> '1000'
		GROUP BY category
		ORDER BY category
	`, from, to, entityIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CashFlowRow
	for rows.Next() {
		var c CashFlowRow
		if err := rows.Scan(&c.Category, &c.NetCash); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CashFlowLine is one reconciling line of the indirect statement.
type CashFlowLine struct {
	Label  string `json:"label"`
	Amount string `json:"amount"`
}

// IndirectCashFlowReport reconciles net income to operating cash (IAS 7 indirect
// method) and reports investing/financing from the activity split.
type IndirectCashFlowReport struct {
	NetIncome    string         `json:"netIncome"`
	Adjustments  []CashFlowLine `json:"adjustments"`
	NetOperating string         `json:"netOperating"`
	NetInvesting string         `json:"netInvesting"`
	NetFinancing string         `json:"netFinancing"`
	NetChange    string         `json:"netChange"`
}

// accountMovementBase returns the net posted movement (debit_base − credit_base)
// of an account over [from, to] for the entity scope.
func (r *Repository) accountMovementBase(ctx context.Context, code string, from, to *time.Time, entityIDs []uuid.UUID) (decimal.Decimal, error) {
	var s string
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(jl.debit_base - jl.credit_base), 0)::text
		FROM journal_lines jl
		JOIN chart_of_accounts coa ON coa.id = jl.account_id
		JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
			AND ($2::date IS NULL OR je.accounting_date >= $2)
			AND ($3::date IS NULL OR je.accounting_date <= $3)
			AND je.entity_id = ANY($4::uuid[])
		WHERE coa.code = $1
	`, code, from, to, entityIDs).Scan(&s)
	if err != nil {
		return decimal.Zero, err
	}
	d, _ := decimal.NewFromString(s)
	return d, nil
}

// IndirectCashFlow builds the indirect-method operating reconciliation and pulls
// investing/financing totals from the direct activity split, so the bottom line
// equals the direct method's net change in cash.
func (r *Repository) IndirectCashFlow(ctx context.Context, from, to *time.Time, entityIDs []uuid.UUID) (IndirectCashFlowReport, error) {
	// Net income = revenue (credit-normal) − expenses (debit-normal).
	var niStr string
	if err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(
			CASE WHEN coa.account_type = 'revenue' THEN jl.credit_base - jl.debit_base
			     WHEN coa.account_type = 'expense' THEN jl.credit_base - jl.debit_base
			     ELSE 0 END), 0)::text
		FROM journal_lines jl
		JOIN chart_of_accounts coa ON coa.id = jl.account_id
		JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
			AND ($1::date IS NULL OR je.accounting_date >= $1)
			AND ($2::date IS NULL OR je.accounting_date <= $2)
			AND je.entity_id = ANY($3::uuid[])
		WHERE coa.account_type IN ('revenue', 'expense')
	`, from, to, entityIDs).Scan(&niStr); err != nil {
		return IndirectCashFlowReport{}, err
	}
	netIncome, _ := decimal.NewFromString(niStr)

	// Non-cash add-backs (in net income but no cash effect).
	depreciation, err := r.accountMovementBase(ctx, "5300", from, to, entityIDs)
	if err != nil {
		return IndirectCashFlowReport{}, err
	}
	impairment, err := r.accountMovementBase(ctx, "5310", from, to, entityIDs)
	if err != nil {
		return IndirectCashFlowReport{}, err
	}
	provisions, err := r.accountMovementBase(ctx, "5500", from, to, entityIDs)
	if err != nil {
		return IndirectCashFlowReport{}, err
	}
	// Working-capital movements. Δ debit-normal asset uses cash (negative); Δ
	// credit-normal liability provides cash (positive).
	arMove, err := r.accountMovementBase(ctx, "1100", from, to, entityIDs)
	if err != nil {
		return IndirectCashFlowReport{}, err
	}
	apMove, err := r.accountMovementBase(ctx, "2000", from, to, entityIDs)
	if err != nil {
		return IndirectCashFlowReport{}, err
	}

	adjustments := []CashFlowLine{
		{Label: "Depreciation", Amount: depreciation.StringFixed(2)},
		{Label: "Impairment", Amount: impairment.StringFixed(2)},
		{Label: "Provisions (non-cash)", Amount: provisions.StringFixed(2)},
		{Label: "Change in receivables", Amount: arMove.Neg().StringFixed(2)},
		{Label: "Change in payables", Amount: apMove.Neg().StringFixed(2)},
	}
	// arMove is debit-normal (increase = cash used) → subtract arMove.
	// apMove is debit-normal sign; a payables increase shows as a net credit
	// (negative debit movement), so −apMove is the cash provided.
	netOperating := netIncome.Add(depreciation).Add(impairment).Add(provisions).Sub(arMove).Sub(apMove)

	// Investing / financing from the direct activity split.
	direct, err := r.CashFlow(ctx, from, to, entityIDs)
	if err != nil {
		return IndirectCashFlowReport{}, err
	}
	investing, financing := decimal.Zero, decimal.Zero
	for _, row := range direct {
		v, _ := decimal.NewFromString(row.NetCash)
		switch row.Category {
		case "investing":
			investing = investing.Add(v)
		case "financing":
			financing = financing.Add(v)
		}
	}

	netChange := netOperating.Add(investing).Add(financing)
	return IndirectCashFlowReport{
		NetIncome:    netIncome.StringFixed(2),
		Adjustments:  adjustments,
		NetOperating: netOperating.StringFixed(2),
		NetInvesting: investing.StringFixed(2),
		NetFinancing: financing.StringFixed(2),
		NetChange:    netChange.StringFixed(2),
	}, nil
}
