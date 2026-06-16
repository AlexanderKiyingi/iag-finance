package repository

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// AccountBalance is one account's natural net balance over a window: revenue is
// credit-positive, expense is debit-positive.
type AccountBalance struct {
	Code    string
	Type    string
	Balance decimal.Decimal
}

// RevenueExpenseBalancesForYear returns each revenue/expense account's net
// POSTED balance for the calendar year (accounting_date within the year),
// used to build the year-end closing entry. Accounts with no activity are
// omitted. Revenue balances are credit-positive; expense balances debit-positive.
func (r *Repository) RevenueExpenseBalancesForYear(ctx context.Context, year int) ([]AccountBalance, error) {
	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC)
	// Base-currency balances: the closing entry posts in base (rate 1) and must
	// zero the same consolidated balances the P&L / trial balance report.
	rows, err := r.pool.Query(ctx, `
		SELECT coa.code, coa.account_type,
			(CASE coa.account_type
				WHEN 'revenue' THEN SUM(jl.credit_base) - SUM(jl.debit_base)
				WHEN 'expense' THEN SUM(jl.debit_base) - SUM(jl.credit_base)
			END)::text
		FROM chart_of_accounts coa
		JOIN journal_lines jl ON jl.account_id = coa.id
		JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
			AND je.accounting_date >= $1 AND je.accounting_date <= $2
		WHERE coa.active = TRUE AND coa.account_type IN ('revenue', 'expense')
		GROUP BY coa.id, coa.code, coa.account_type
		HAVING SUM(jl.debit_base) <> 0 OR SUM(jl.credit_base) <> 0
		ORDER BY coa.code
	`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccountBalance
	for rows.Next() {
		var b AccountBalance
		var bal string
		if err := rows.Scan(&b.Code, &b.Type, &bal); err != nil {
			return nil, err
		}
		d, err := decimal.NewFromString(bal)
		if err != nil {
			return nil, err
		}
		b.Balance = d
		out = append(out, b)
	}
	return out, rows.Err()
}

// CountDraftEntriesInYear counts unposted entries dated within the calendar
// year — a year-end close refuses while any exist, so nothing is orphaned
// outside the closed books.
func (r *Repository) CountDraftEntriesInYear(ctx context.Context, year int) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM journal_entries
		WHERE status = 'draft' AND EXTRACT(YEAR FROM accounting_date) = $1
	`, year).Scan(&n)
	return n, err
}
