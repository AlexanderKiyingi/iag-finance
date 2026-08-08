package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// LeaveProvision is a stated movement in the accrued-leave liability and the
// journal entry it booked.
type LeaveProvision struct {
	ID             uuid.UUID  `json:"id"`
	ProvisionRef   string     `json:"provisionRef"`
	Period         string     `json:"period"`
	Amount         string     `json:"amount"`
	Currency       string     `json:"currency"`
	Note           string     `json:"note,omitempty"`
	JournalEntryID *uuid.UUID `json:"journalEntryId,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

// LeaveProvisionParams records a booked provision.
type LeaveProvisionParams struct {
	ProvisionRef   string
	Period         string
	Amount         string
	Currency       string
	Note           string
	JournalEntryID uuid.UUID
}

func scanLeaveProvision(row pgx.Row) (*LeaveProvision, error) {
	var p LeaveProvision
	if err := row.Scan(&p.ID, &p.ProvisionRef, &p.Period, &p.Amount,
		&p.Currency, &p.Note, &p.JournalEntryID, &p.CreatedAt); err != nil {
		return nil, err
	}
	return &p, nil
}

const leaveProvisionCols = `id, provision_ref, period, amount::text, currency, note, journal_entry_id, created_at`

// GetLeaveProvisionByRef returns a provision by its idempotency key, or nil.
func (r *Repository) GetLeaveProvisionByRef(ctx context.Context, ref string) (*LeaveProvision, error) {
	p, err := scanLeaveProvision(r.pool.QueryRow(ctx,
		`SELECT `+leaveProvisionCols+` FROM payroll_leave_provisions WHERE provision_ref = $1`, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// RecordLeaveProvision stores a booked provision.
func (r *Repository) RecordLeaveProvision(ctx context.Context, in LeaveProvisionParams) (*LeaveProvision, error) {
	return scanLeaveProvision(r.pool.QueryRow(ctx, `
		INSERT INTO payroll_leave_provisions (provision_ref, period, amount, currency, note, journal_entry_id)
		VALUES ($1, $2, $3::numeric, $4, $5, $6)
		RETURNING `+leaveProvisionCols,
		in.ProvisionRef, in.Period, in.Amount, in.Currency, in.Note, in.JournalEntryID))
}

// ListLeaveProvisions returns provisions newest first, optionally for one period.
func (r *Repository) ListLeaveProvisions(ctx context.Context, period string, limit int) ([]LeaveProvision, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+leaveProvisionCols+`
		FROM payroll_leave_provisions
		WHERE ($1 = '' OR period = $1)
		ORDER BY created_at DESC
		LIMIT $2`, period, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LeaveProvision{}
	for rows.Next() {
		p, err := scanLeaveProvision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}
