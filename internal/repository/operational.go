package repository

import (
	"context"

	"github.com/iag-finance/backend/internal/domain"
)

func (r *Repository) ListBankAccounts(ctx context.Context) ([]domain.BankAccount, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, code, name, institution, currency, balance::text, status_label, purpose, created_at, updated_at
		FROM bank_accounts
		ORDER BY code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.BankAccount
	for rows.Next() {
		var b domain.BankAccount
		if err := rows.Scan(&b.ID, &b.Code, &b.Name, &b.Institution, &b.Currency, &b.Balance, &b.StatusLabel, &b.Purpose, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *Repository) ListCherryIntake(ctx context.Context, limit int) ([]domain.CherryIntakeLine, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, intake_code, farmer_name, qty_kg::text, amount_ugx::text, status_label, created_at, updated_at
		FROM cherry_intake_lines
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CherryIntakeLine
	for rows.Next() {
		var line domain.CherryIntakeLine
		if err := rows.Scan(&line.ID, &line.IntakeCode, &line.FarmerName, &line.QtyKg, &line.AmountUgx, &line.StatusLabel, &line.CreatedAt, &line.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, line)
	}
	return out, rows.Err()
}
