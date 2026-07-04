package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// IFRS 10 consolidation eliminations (report time).
//
// Consolidated reports aggregate across the entity scope; these functions compute
// the adjustments that remove intra-group activity so the group isn't double-
// counted. Two kinds:
//
//   * Transactional — any POSTED entry whose entity AND counterparty are both in
//     the consolidation scope is intra-group; its lines are eliminated per account
//     (this nets IC receivables/payables AND IC revenue/COGS at once). Entries that
//     touch a consolidation control account (Investment/Goodwill/NCI) are skipped
//     here and handled structurally, so an acquisition's cash leg isn't eliminated.
//
//   * Structural — for each subsidiary in scope whose parent is in scope, eliminate
//     the parent's Investment against the subsidiary's equity, recognise the
//     non-controlling interest for the un-owned share, and carry the residual as
//     goodwill (IFRS 3, acquisition-date, NCI at proportionate share of net assets).
//     Post-acquisition NCI share of profits, goodwill impairment and fair-value
//     step-ups are out of scope for this pass.

// Consolidation control account codes (never transactionally eliminated).
const (
	investmentCode = "1800"
	goodwillCode   = "1900"
	nciCode        = "3200"
)

// EliminationRow is one consolidation adjustment, signed in the account type's
// natural report direction so it can be added straight onto the aggregated
// statement (asset/expense: debit-normal; liability/equity/revenue: credit-normal).
type EliminationRow struct {
	AccountCode string `json:"accountCode"`
	AccountName string `json:"accountName"`
	AccountType string `json:"accountType"`
	Amount      string `json:"amount"`
}

// TransactionalEliminations returns, per account, the reversal of intra-group
// posted lines over [from, to] for the scope — the negative of each account's
// intra-group balance in its natural sign, so adding the rows cancels the
// intra-group activity. from may be nil (cumulative, for the balance sheet).
func (r *Repository) TransactionalEliminations(ctx context.Context, from, to *time.Time, scope []uuid.UUID) ([]EliminationRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT coa.code, coa.name, coa.account_type,
			CASE coa.account_type
				WHEN 'asset'   THEN SUM(jl.debit_base  - jl.credit_base)
				WHEN 'expense' THEN SUM(jl.debit_base  - jl.credit_base)
				ELSE                SUM(jl.credit_base - jl.debit_base)
			END::text AS natural_balance
		FROM journal_lines jl
		JOIN chart_of_accounts coa ON coa.id = jl.account_id
		JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
			AND je.entity_id = ANY($1::uuid[])
			AND je.counterparty_entity_id = ANY($1::uuid[])
			AND ($2::date IS NULL OR je.accounting_date >= $2)
			AND ($3::date IS NULL OR je.accounting_date <= $3)
			AND je.id NOT IN (
				SELECT jl2.journal_entry_id FROM journal_lines jl2
				JOIN chart_of_accounts c2 ON c2.id = jl2.account_id
				WHERE c2.code IN ('1800', '1900', '3200')
			)
		GROUP BY coa.id, coa.code, coa.name, coa.account_type
		HAVING SUM(jl.debit_base) <> 0 OR SUM(jl.credit_base) <> 0
		ORDER BY coa.account_type, coa.code
	`, scope, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EliminationRow
	for rows.Next() {
		var code, name, atype, bal string
		if err := rows.Scan(&code, &name, &atype, &bal); err != nil {
			return nil, err
		}
		d, _ := decimal.NewFromString(bal)
		if d.IsZero() {
			continue
		}
		out = append(out, EliminationRow{
			AccountCode: code,
			AccountName: "Eliminate intercompany " + name,
			AccountType: atype,
			Amount:      d.Neg().StringFixed(2),
		})
	}
	return out, rows.Err()
}

// subsidiaryInScope is a parent→subsidiary pair both inside the scope.
type subsidiaryInScope struct {
	SubID    uuid.UUID
	SubCode  string
	ParentID uuid.UUID
	Owned    decimal.Decimal // ownership fraction the parent holds
}

// subsidiariesInScope returns the entities in scope whose parent is also in scope.
func (r *Repository) subsidiariesInScope(ctx context.Context, scope []uuid.UUID) ([]subsidiaryInScope, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT e.id, e.code, e.parent_id, e.ownership_pct::text
		FROM entities e
		WHERE e.id = ANY($1::uuid[]) AND e.parent_id = ANY($1::uuid[])
		ORDER BY e.code
	`, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []subsidiaryInScope
	for rows.Next() {
		var s subsidiaryInScope
		var owned string
		if err := rows.Scan(&s.SubID, &s.SubCode, &s.ParentID, &owned); err != nil {
			return nil, err
		}
		s.Owned, _ = decimal.NewFromString(owned)
		out = append(out, s)
	}
	return out, rows.Err()
}

// subEquity returns a subsidiary's equity-account balances as of the date (its
// share capital / retained earnings etc.; current-period earnings stay in the
// group's consolidated earnings line).
func (r *Repository) subEquity(ctx context.Context, subID uuid.UUID, asOf *time.Time) (decimal.Decimal, error) {
	var s string
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(jl.credit_base - jl.debit_base), 0)::text
		FROM journal_lines jl
		JOIN chart_of_accounts coa ON coa.id = jl.account_id
		JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
			AND je.entity_id = $1
			AND ($2::date IS NULL OR je.accounting_date <= $2)
		WHERE coa.account_type = 'equity'
	`, subID, asOf).Scan(&s)
	if err != nil {
		return decimal.Zero, err
	}
	d, _ := decimal.NewFromString(s)
	return d, nil
}

// investmentInSub returns the parent's carrying amount of its investment in the
// subsidiary — the balance of the Investment account (1800) on parent entries
// tagged with the subsidiary as counterparty, as of the date.
func (r *Repository) investmentInSub(ctx context.Context, parentID, subID uuid.UUID, asOf *time.Time) (decimal.Decimal, error) {
	var s string
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(jl.debit_base - jl.credit_base), 0)::text
		FROM journal_lines jl
		JOIN chart_of_accounts coa ON coa.id = jl.account_id
		JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
			AND je.entity_id = $1 AND je.counterparty_entity_id = $2
			AND ($3::date IS NULL OR je.accounting_date <= $3)
		WHERE coa.code = $4
	`, parentID, subID, asOf, investmentCode).Scan(&s)
	if err != nil {
		return decimal.Zero, err
	}
	d, _ := decimal.NewFromString(s)
	return d, nil
}

// investmentEquityRows is the pure elimination math for one subsidiary: eliminate
// the whole subsidiary equity, add back NCI for the un-owned share, remove the
// investment and carry the residual as goodwill. The rows balance — assets and
// equity each net to −(ownership × subEquity).
func investmentEquityRows(subCode string, invest, subEquity, ownership decimal.Decimal) (rows []EliminationRow, nci, goodwill decimal.Decimal) {
	ownedEquity := subEquity.Mul(ownership).Round(2) // o × E, the parent's share
	nci = subEquity.Sub(ownedEquity).Round(2)        // (1 − o) × E
	goodwill = invest.Sub(ownedEquity).Round(2)      // I − o × E (negative = bargain purchase)
	rows = []EliminationRow{
		{AccountCode: investmentCode, AccountName: "Eliminate investment in " + subCode, AccountType: "asset", Amount: invest.Neg().StringFixed(2)},
		{AccountCode: goodwillCode, AccountName: "Goodwill on " + subCode, AccountType: "asset", Amount: goodwill.StringFixed(2)},
		{AccountCode: "ELIM-EQ", AccountName: "Eliminate equity of " + subCode, AccountType: "equity", Amount: subEquity.Neg().StringFixed(2)},
		{AccountCode: nciCode, AccountName: "Non-controlling interest (" + subCode + ")", AccountType: "equity", Amount: nci.StringFixed(2)},
	}
	return rows, nci, goodwill
}

// StructuralEliminations returns the investment/equity/NCI/goodwill adjustments
// for every subsidiary in scope as of the date, plus the total NCI and goodwill.
func (r *Repository) StructuralEliminations(ctx context.Context, asOf *time.Time, scope []uuid.UUID) (rows []EliminationRow, totalNCI, totalGoodwill decimal.Decimal, err error) {
	subs, err := r.subsidiariesInScope(ctx, scope)
	if err != nil {
		return nil, decimal.Zero, decimal.Zero, err
	}
	totalNCI, totalGoodwill = decimal.Zero, decimal.Zero
	for _, sub := range subs {
		equity, err := r.subEquity(ctx, sub.SubID, asOf)
		if err != nil {
			return nil, decimal.Zero, decimal.Zero, err
		}
		invest, err := r.investmentInSub(ctx, sub.ParentID, sub.SubID, asOf)
		if err != nil {
			return nil, decimal.Zero, decimal.Zero, err
		}
		if equity.IsZero() && invest.IsZero() {
			continue
		}
		subRows, nci, goodwill := investmentEquityRows(sub.SubCode, invest, equity, sub.Owned)
		rows = append(rows, subRows...)
		totalNCI = totalNCI.Add(nci)
		totalGoodwill = totalGoodwill.Add(goodwill)
	}
	return rows, totalNCI, totalGoodwill, nil
}

// ConsolidationSummary is the full set of eliminations for a scope as of a date,
// used by the /consolidation/eliminations endpoint.
type ConsolidationSummary struct {
	Transactional []EliminationRow `json:"transactional"`
	Structural    []EliminationRow `json:"structural"`
	NCI           string           `json:"nci"`
	Goodwill      string           `json:"goodwill"`
}

// ConsolidationEliminations assembles the balance-sheet-basis summary (cumulative
// through asOf) for a scope.
func (r *Repository) ConsolidationEliminations(ctx context.Context, asOf *time.Time, scope []uuid.UUID) (ConsolidationSummary, error) {
	txn, err := r.TransactionalEliminations(ctx, nil, asOf, scope)
	if err != nil {
		return ConsolidationSummary{}, err
	}
	structural, nci, goodwill, err := r.StructuralEliminations(ctx, asOf, scope)
	if err != nil {
		return ConsolidationSummary{}, err
	}
	return ConsolidationSummary{
		Transactional: txn,
		Structural:    structural,
		NCI:           nci.StringFixed(2),
		Goodwill:      goodwill.StringFixed(2),
	}, nil
}
