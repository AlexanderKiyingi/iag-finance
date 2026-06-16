package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// FiscalPeriod is one month's posting status in the general ledger.
type FiscalPeriod struct {
	Period    string     `json:"period"`
	Status    string     `json:"status"`
	ClosedAt  *time.Time `json:"closedAt,omitempty"`
	ClosedBy  *uuid.UUID `json:"closedBy,omitempty"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// IsPeriodClosed reports whether postings into the given 'YYYY-MM' period are
// blocked. Periods are open by default: a missing row means open.
func (r *Repository) IsPeriodClosed(ctx context.Context, period string) (bool, error) {
	var status string
	err := r.pool.QueryRow(ctx,
		`SELECT status FROM fiscal_periods WHERE period = $1`, period,
	).Scan(&status)
	if err != nil {
		// No row → period has never been closed → open.
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return status == "closed", nil
}

// ErrPeriodHasDrafts blocks closing a period that still has unposted entries
// dated within it — they would be orphaned outside the closed books.
var ErrPeriodHasDrafts = errors.New("period has draft entries; post or delete them before closing")

// SetPeriodStatus upserts a period's open/closed status. closed_at/closed_by
// are stamped when closing and cleared when reopening. Closing refuses if any
// draft entry is dated within the period.
func (r *Repository) SetPeriodStatus(ctx context.Context, period, status string, by *uuid.UUID) error {
	if status == "closed" {
		var drafts int
		if err := r.pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM journal_entries
			WHERE status = 'draft' AND to_char(accounting_date, 'YYYY-MM') = $1
		`, period).Scan(&drafts); err != nil {
			return err
		}
		if drafts > 0 {
			return ErrPeriodHasDrafts
		}
		_, err := r.pool.Exec(ctx, `
			INSERT INTO fiscal_periods (period, status, closed_at, closed_by, updated_at)
			VALUES ($1, 'closed', NOW(), $2, NOW())
			ON CONFLICT (period) DO UPDATE
			SET status = 'closed', closed_at = NOW(), closed_by = $2, updated_at = NOW()
		`, period, by)
		return err
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO fiscal_periods (period, status, closed_at, closed_by, updated_at)
		VALUES ($1, 'open', NULL, NULL, NOW())
		ON CONFLICT (period) DO UPDATE
		SET status = 'open', closed_at = NULL, closed_by = NULL, updated_at = NOW()
	`, period)
	return err
}

// ListPeriods returns every period that has an explicit status row, newest
// first. Periods with no row are open by default and simply absent here.
func (r *Repository) ListPeriods(ctx context.Context) ([]FiscalPeriod, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT period, status, closed_at, closed_by, updated_at
		FROM fiscal_periods
		ORDER BY period DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FiscalPeriod{}
	for rows.Next() {
		var p FiscalPeriod
		if err := rows.Scan(&p.Period, &p.Status, &p.ClosedAt, &p.ClosedBy, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
