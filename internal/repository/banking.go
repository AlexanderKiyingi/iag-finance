package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type LegacyBankAccount struct {
	Name     string  `json:"name"`
	Balance  float64 `json:"balance"`
	InBooks  float64 `json:"inBooks"`
	Review   int     `json:"review"`
	Type     string  `json:"type"`
	Currency string  `json:"currency,omitempty"`
}

type LegacyBankTx struct {
	Date     string   `json:"date"`
	Desc     string   `json:"desc"`
	Payee    string   `json:"payee"`
	Category string   `json:"category"`
	Spent    *float64 `json:"spent"`
	Received *float64 `json:"received"`
	Action   string   `json:"action"`
	Matched  *string  `json:"matched"`
}

func (r *Repository) ListLegacyBankAccounts(ctx context.Context) ([]LegacyBankAccount, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT name, balance::float8, balance::float8, 0, purpose, currency
		FROM bank_accounts ORDER BY code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LegacyBankAccount
	for rows.Next() {
		var a LegacyBankAccount
		if err := rows.Scan(&a.Name, &a.Balance, &a.InBooks, &a.Review, &a.Type, &a.Currency); err != nil {
			return nil, err
		}
		if a.Type == "" {
			a.Type = "checking"
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repository) ListLegacyBankTx(ctx context.Context, limit, offset int) ([]LegacyBankTx, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bank_transactions`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT txn_date, description, payee, category, spent, received, action_label, matched_ref
		FROM bank_transactions
		ORDER BY txn_date DESC LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []LegacyBankTx
	for rows.Next() {
		var tx LegacyBankTx
		var dt time.Time
		var spent, received *float64
		if err := rows.Scan(&dt, &tx.Desc, &tx.Payee, &tx.Category, &spent, &received, &tx.Action, &tx.Matched); err != nil {
			return nil, 0, err
		}
		tx.Date = dt.Format("2006-01-02")
		tx.Spent = spent
		tx.Received = received
		out = append(out, tx)
	}
	return out, total, rows.Err()
}

func (r *Repository) UpdateBankStatementStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := r.pool.Exec(ctx, `UPDATE bank_statements SET status = $2, updated_at = NOW() WHERE id = $1`, id, status)
	return err
}
