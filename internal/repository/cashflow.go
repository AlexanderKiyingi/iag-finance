package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
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
