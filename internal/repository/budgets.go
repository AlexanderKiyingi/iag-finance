package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// UpsertBudget sets the budgeted base-currency amount for an account in a period
// (YYYY-MM), for the entity in context.
func (r *Repository) UpsertBudget(ctx context.Context, period, accountCode, amount string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO budgets (entity_id, period, account_code, amount)
		VALUES ($1, $2, $3, $4::numeric)
		ON CONFLICT (entity_id, period, account_code)
		DO UPDATE SET amount = EXCLUDED.amount, updated_at = NOW()
	`, EntityFromContext(ctx), period, accountCode, amount)
	return err
}

// BudgetLine is one account's budget vs actual for the window.
type BudgetLine struct {
	AccountCode string `json:"accountCode"`
	AccountName string `json:"accountName"`
	AccountType string `json:"accountType"`
	Budget      string `json:"budget"`
	Actual      string `json:"actual"`
	Variance    string `json:"variance"` // actual − budget
}

// BudgetVsActual compares budgeted vs actual (posted, base-currency) amounts per
// account over [from, to] for the given entity scope.
func (r *Repository) BudgetVsActual(ctx context.Context, from, to *time.Time, entityIDs []uuid.UUID) ([]BudgetLine, error) {
	rows, err := r.pool.Query(ctx, `
		WITH actual AS (
			SELECT coa.code,
				CASE coa.account_type
					WHEN 'revenue' THEN SUM(jl.credit_base - jl.debit_base)
					WHEN 'liability' THEN SUM(jl.credit_base - jl.debit_base)
					WHEN 'equity' THEN SUM(jl.credit_base - jl.debit_base)
					ELSE SUM(jl.debit_base - jl.credit_base)
				END AS amt
			FROM chart_of_accounts coa
			JOIN journal_lines jl ON jl.account_id = coa.id
			JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
				AND ($1::date IS NULL OR je.accounting_date >= $1)
				AND ($2::date IS NULL OR je.accounting_date <= $2)
				AND je.entity_id = ANY($3::uuid[])
			-- account_type is the CASE discriminator, so it must be grouped too;
			-- coa.code is UNIQUE (not the PK), so Postgres won't infer the
			-- functional dependency and rejects grouping by code alone (42803).
			GROUP BY coa.code, coa.account_type
		),
		bud AS (
			SELECT account_code AS code, SUM(amount) AS amt
			FROM budgets
			WHERE entity_id = ANY($3::uuid[])
				AND ($1::date IS NULL OR period >= to_char($1::date, 'YYYY-MM'))
				AND ($2::date IS NULL OR period <= to_char($2::date, 'YYYY-MM'))
			GROUP BY account_code
		)
		SELECT coa.code, coa.name, coa.account_type,
			COALESCE(b.amt, 0)::text,
			COALESCE(a.amt, 0)::text,
			(COALESCE(a.amt, 0) - COALESCE(b.amt, 0))::text
		FROM chart_of_accounts coa
		LEFT JOIN actual a ON a.code = coa.code
		LEFT JOIN bud b ON b.code = coa.code
		WHERE coa.active = TRUE AND (a.amt IS NOT NULL OR b.amt IS NOT NULL)
		ORDER BY coa.code
	`, from, to, entityIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BudgetLine
	for rows.Next() {
		var l BudgetLine
		if err := rows.Scan(&l.AccountCode, &l.AccountName, &l.AccountType, &l.Budget, &l.Actual, &l.Variance); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
