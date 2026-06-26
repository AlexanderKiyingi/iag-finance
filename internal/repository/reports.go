package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type ARAgingBucket struct {
	Label  string `json:"label"`
	Amount string `json:"amount"`
	Count  int    `json:"count"`
}

type PLRow struct {
	AccountCode string `json:"accountCode"`
	AccountName string `json:"accountName"`
	AccountType string `json:"accountType"` // "revenue" | "expense"
	Amount      string `json:"amount"`
}

type BalanceSheetSection struct {
	AccountType string `json:"accountType"`
	AccountCode string `json:"accountCode"`
	AccountName string `json:"accountName"`
	Balance     string `json:"balance"`
}

func (r *Repository) ARAging(ctx context.Context) ([]ARAgingBucket, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			CASE
				WHEN due_date IS NULL OR due_date >= CURRENT_DATE THEN 'current'
				WHEN due_date >= CURRENT_DATE - INTERVAL '30 days' THEN '1-30'
				WHEN due_date >= CURRENT_DATE - INTERVAL '60 days' THEN '31-60'
				WHEN due_date >= CURRENT_DATE - INTERVAL '90 days' THEN '61-90'
				ELSE '90+'
			END AS bucket,
			COALESCE(SUM((amount - amount_paid) * fx_rate), 0)::text AS open_amount,
			COUNT(*)::int
		FROM ar_open_items
		WHERE status IN ('open', 'partial')
		GROUP BY 1
		ORDER BY 1
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	order := []string{"current", "1-30", "31-60", "61-90", "90+"}
	found := map[string]ARAgingBucket{}
	for rows.Next() {
		var label, amount string
		var count int
		if err := rows.Scan(&label, &amount, &count); err != nil {
			return nil, err
		}
		found[label] = ARAgingBucket{Label: label, Amount: amount, Count: count}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ARAgingBucket, 0, len(order))
	for _, label := range order {
		if b, ok := found[label]; ok {
			out = append(out, b)
		} else {
			out = append(out, ARAgingBucket{Label: label, Amount: "0", Count: 0})
		}
	}
	return out, nil
}

// ProfitAndLoss reports revenue/expense activity, optionally bounded to a
// [from, to] accounting-date range (nil = unbounded on that side). Only POSTED
// entries are summed: the join is an INNER JOIN onto a posted, in-range entry,
// so the date bounds actually filter the totals and still-draft lines never leak
// into the statement.
func (r *Repository) ProfitAndLoss(ctx context.Context, from, to *time.Time, entityIDs []uuid.UUID) ([]PLRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT coa.code, coa.name, coa.account_type,
			CASE coa.account_type
				WHEN 'revenue' THEN (SUM(jl.credit_base) - SUM(jl.debit_base))::text
				WHEN 'expense' THEN (SUM(jl.debit_base) - SUM(jl.credit_base))::text
			END
		FROM chart_of_accounts coa
		JOIN journal_lines jl ON jl.account_id = coa.id
		JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
			AND ($1::date IS NULL OR je.accounting_date >= $1)
			AND ($2::date IS NULL OR je.accounting_date <= $2)
			AND je.entity_id = ANY($3::uuid[])
		WHERE coa.active = TRUE AND coa.account_type IN ('revenue', 'expense')
		GROUP BY coa.id, coa.code, coa.name, coa.account_type
		HAVING SUM(jl.debit_base) > 0 OR SUM(jl.credit_base) > 0
		ORDER BY coa.account_type, coa.code
	`, from, to, entityIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PLRow
	for rows.Next() {
		var row PLRow
		if err := rows.Scan(&row.AccountCode, &row.AccountName, &row.AccountType, &row.Amount); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// NetIncome returns revenue minus expense (profit positive) over the [from, to]
// window across POSTED entries. It is the amount that must roll into equity for
// the balance sheet to balance before a year-end closing entry exists. It uses
// the same INNER-JOIN-on-posted semantics as BalanceSheet so the two stay
// consistent and Assets = Liabilities + Equity holds exactly.
func (r *Repository) NetIncome(ctx context.Context, from, to *time.Time, entityIDs []uuid.UUID) (decimal.Decimal, error) {
	var s string
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(
			CASE coa.account_type
				WHEN 'revenue' THEN jl.credit_base - jl.debit_base
				WHEN 'expense' THEN jl.credit_base - jl.debit_base
			END
		), 0)::text
		FROM chart_of_accounts coa
		JOIN journal_lines jl ON jl.account_id = coa.id
		JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
			AND ($1::date IS NULL OR je.accounting_date >= $1)
			AND ($2::date IS NULL OR je.accounting_date <= $2)
			AND je.entity_id = ANY($3::uuid[])
		WHERE coa.active = TRUE AND coa.account_type IN ('revenue', 'expense')
	`, from, to, entityIDs).Scan(&s)
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromString(s)
}

// BalanceSheet reports asset/liability/equity balances, optionally "as of" a
// date (cumulative through asOf; nil = through today). It appends a synthetic
// "Current Period Earnings" equity line carrying net income not yet moved to
// retained earnings by a closing entry, so the statement balances even before
// year-end close. After a close the P&L nets to zero and that line is zero (the
// profit then lives in the Retained Earnings account). Only posted entries count.
func (r *Repository) BalanceSheet(ctx context.Context, asOf *time.Time, entityIDs []uuid.UUID) ([]BalanceSheetSection, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT coa.account_type, coa.code, coa.name,
			CASE coa.account_type
				WHEN 'asset' THEN (SUM(jl.debit_base) - SUM(jl.credit_base))::text
				WHEN 'liability' THEN (SUM(jl.credit_base) - SUM(jl.debit_base))::text
				WHEN 'equity' THEN (SUM(jl.credit_base) - SUM(jl.debit_base))::text
			END
		FROM chart_of_accounts coa
		JOIN journal_lines jl ON jl.account_id = coa.id
		JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
			AND ($1::date IS NULL OR je.accounting_date <= $1)
			AND je.entity_id = ANY($2::uuid[])
		WHERE coa.active = TRUE AND coa.account_type IN ('asset', 'liability', 'equity')
		GROUP BY coa.id, coa.account_type, coa.code, coa.name
		HAVING SUM(jl.debit_base) > 0 OR SUM(jl.credit_base) > 0
		ORDER BY coa.account_type, coa.code
	`, asOf, entityIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BalanceSheetSection
	for rows.Next() {
		var row BalanceSheetSection
		if err := rows.Scan(&row.AccountType, &row.AccountCode, &row.AccountName, &row.Balance); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	ni, err := r.NetIncome(ctx, nil, asOf, entityIDs)
	if err != nil {
		return nil, err
	}
	if !ni.IsZero() {
		out = append(out, BalanceSheetSection{
			AccountType: "equity",
			AccountCode: "NET-INCOME",
			AccountName: "Current Period Earnings",
			Balance:     ni.String(),
		})
	}
	return out, nil
}

// APAging buckets open payables by how overdue they are, mirroring ARAging.
func (r *Repository) APAging(ctx context.Context) ([]ARAgingBucket, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			CASE
				WHEN due_date IS NULL OR due_date >= CURRENT_DATE THEN 'current'
				WHEN due_date >= CURRENT_DATE - INTERVAL '30 days' THEN '1-30'
				WHEN due_date >= CURRENT_DATE - INTERVAL '60 days' THEN '31-60'
				WHEN due_date >= CURRENT_DATE - INTERVAL '90 days' THEN '61-90'
				ELSE '90+'
			END AS bucket,
			COALESCE(SUM((amount - amount_paid) * fx_rate), 0)::text AS open_amount,
			COUNT(*)::int
		FROM ap_open_items
		WHERE status IN ('open', 'partial')
		GROUP BY 1
		ORDER BY 1
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	order := []string{"current", "1-30", "31-60", "61-90", "90+"}
	found := map[string]ARAgingBucket{}
	for rows.Next() {
		var label, amount string
		var count int
		if err := rows.Scan(&label, &amount, &count); err != nil {
			return nil, err
		}
		found[label] = ARAgingBucket{Label: label, Amount: amount, Count: count}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ARAgingBucket, 0, len(order))
	for _, label := range order {
		if b, ok := found[label]; ok {
			out = append(out, b)
		} else {
			out = append(out, ARAgingBucket{Label: label, Amount: "0", Count: 0})
		}
	}
	return out, nil
}

// GLAccountLine is one posting in an account's subledger. Amounts and the
// running debit-positive balance are in base currency so the closing balance
// ties to the (base-currency) trial balance; Currency names the transaction
// currency the line was originally booked in.
type GLAccountLine struct {
	Date        time.Time `json:"date"`
	EntryNumber string    `json:"entryNumber"`
	Description string    `json:"description"`
	Memo        string    `json:"memo"`
	Currency    string    `json:"currency"`
	Debit       string    `json:"debit"`
	Credit      string    `json:"credit"`
	Balance     string    `json:"balance"`
}

// GLAccountDetail returns the posted postings against one account code over the
// [from, to] window, chronologically, with a running debit-positive base-currency
// balance.
func (r *Repository) GLAccountDetail(ctx context.Context, code string, from, to *time.Time) ([]GLAccountLine, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT je.accounting_date, je.entry_number, je.description, jl.memo, jl.currency,
			jl.debit_base::text, jl.credit_base::text, jl.debit_base, jl.credit_base
		FROM journal_lines jl
		JOIN chart_of_accounts coa ON coa.id = jl.account_id
		JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
			AND ($2::date IS NULL OR je.accounting_date >= $2)
			AND ($3::date IS NULL OR je.accounting_date <= $3)
		WHERE coa.code = $1
		ORDER BY je.accounting_date, je.entry_number, jl.line_order
	`, code, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GLAccountLine
	balance := decimal.Zero
	for rows.Next() {
		var l GLAccountLine
		var debit, credit decimal.Decimal
		if err := rows.Scan(&l.Date, &l.EntryNumber, &l.Description, &l.Memo, &l.Currency, &l.Debit, &l.Credit, &debit, &credit); err != nil {
			return nil, err
		}
		balance = balance.Add(debit).Sub(credit)
		l.Balance = balance.String()
		out = append(out, l)
	}
	return out, rows.Err()
}

// FinanceSummary aggregates AR metrics for BFF consumers (e.g. DMS).
type FinanceSummary struct {
	ARBalance    string    `json:"arBalance"`
	Overdue      string    `json:"overdue"`
	Collected    string    `json:"collected"`
	OpenItems    int       `json:"openItems"`
	GeneratedAt  time.Time `json:"generatedAt"`
}

func (r *Repository) FinanceSummary(ctx context.Context) (FinanceSummary, error) {
	var s FinanceSummary
	s.GeneratedAt = time.Now().UTC()
	err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM((amount - amount_paid) * fx_rate), 0)::text,
			COALESCE(SUM(CASE WHEN due_date < CURRENT_DATE THEN (amount - amount_paid) * fx_rate ELSE 0 END), 0)::text,
			COALESCE(SUM(amount_paid * fx_rate), 0)::text,
			COUNT(*) FILTER (WHERE status IN ('open', 'partial'))::int
		FROM ar_open_items
	`).Scan(&s.ARBalance, &s.Overdue, &s.Collected, &s.OpenItems)
	return s, err
}
