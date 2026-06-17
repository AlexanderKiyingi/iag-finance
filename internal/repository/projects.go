package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Dimension is a project or cost-centre master record.
type Dimension struct {
	ID     uuid.UUID `json:"id"`
	Code   string    `json:"code"`
	Name   string    `json:"name"`
	Active bool      `json:"active"`
}

func (r *Repository) createDimension(ctx context.Context, table, code, name string) (*Dimension, error) {
	var d Dimension
	err := r.pool.QueryRow(ctx,
		"INSERT INTO "+table+" (entity_id, code, name) VALUES ($1, $2, $3) RETURNING id, code, name, active",
		EntityFromContext(ctx), code, name).Scan(&d.ID, &d.Code, &d.Name, &d.Active)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *Repository) listDimensions(ctx context.Context, table string) ([]Dimension, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT id, code, name, active FROM "+table+" WHERE entity_id = $1 ORDER BY code",
		EntityFromContext(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Dimension
	for rows.Next() {
		var d Dimension
		if err := rows.Scan(&d.ID, &d.Code, &d.Name, &d.Active); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Repository) CreateProject(ctx context.Context, code, name string) (*Dimension, error) {
	return r.createDimension(ctx, "projects", code, name)
}
func (r *Repository) ListProjects(ctx context.Context) ([]Dimension, error) {
	return r.listDimensions(ctx, "projects")
}
func (r *Repository) CreateCostCenter(ctx context.Context, code, name string) (*Dimension, error) {
	return r.createDimension(ctx, "cost_centers", code, name)
}
func (r *Repository) ListCostCenters(ctx context.Context) ([]Dimension, error) {
	return r.listDimensions(ctx, "cost_centers")
}

// ProjectPL is the revenue/expense detail for a single project over a window.
func (r *Repository) ProjectPL(ctx context.Context, projectID uuid.UUID, from, to *time.Time) ([]PLRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT coa.code, coa.name,
			CASE coa.account_type
				WHEN 'revenue' THEN (SUM(jl.credit_base) - SUM(jl.debit_base))::text
				WHEN 'expense' THEN (SUM(jl.debit_base) - SUM(jl.credit_base))::text
			END
		FROM chart_of_accounts coa
		JOIN journal_lines jl ON jl.account_id = coa.id AND jl.project_id = $1
		JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
			AND ($2::date IS NULL OR je.accounting_date >= $2)
			AND ($3::date IS NULL OR je.accounting_date <= $3)
		WHERE coa.account_type IN ('revenue', 'expense')
		GROUP BY coa.id, coa.code, coa.name, coa.account_type
		HAVING SUM(jl.debit_base) > 0 OR SUM(jl.credit_base) > 0
		ORDER BY coa.account_type, coa.code
	`, projectID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PLRow
	for rows.Next() {
		var row PLRow
		if err := rows.Scan(&row.AccountCode, &row.AccountName, &row.Amount); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
