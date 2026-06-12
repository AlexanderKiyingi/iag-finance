package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PayrollRun is a finalized payroll run and the journal entry it booked.
type PayrollRun struct {
	ID              uuid.UUID  `json:"id"`
	RunRef          string     `json:"runRef"`
	Period          string     `json:"period"`
	Gross           string     `json:"gross"`
	PAYE            string     `json:"paye"`
	NSSF            string     `json:"nssf"`
	OtherDeductions string     `json:"otherDeductions"`
	Net             string     `json:"net"`
	Currency        string     `json:"currency"`
	Status          string     `json:"status"`
	JournalEntryID  *uuid.UUID `json:"journalEntryId,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
}

// GetPayrollRunByRef returns the run with the given idempotency key, or nil if
// it has not been posted yet.
func (r *Repository) GetPayrollRunByRef(ctx context.Context, runRef string) (*PayrollRun, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, run_ref, period, gross::text, paye::text, nssf::text,
		       other_deductions::text, net::text, currency, status, journal_entry_id, created_at
		FROM payroll_runs WHERE run_ref = $1
	`, runRef)
	var p PayrollRun
	if err := row.Scan(&p.ID, &p.RunRef, &p.Period, &p.Gross, &p.PAYE, &p.NSSF,
		&p.OtherDeductions, &p.Net, &p.Currency, &p.Status, &p.JournalEntryID, &p.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// PayrollRunParams is the persisted record of a posted run.
type PayrollRunParams struct {
	RunRef          string
	Period          string
	Gross           string
	PAYE            string
	NSSF            string
	OtherDeductions string
	Net             string
	Currency        string
	JournalEntryID  uuid.UUID
}

// RecordPayrollRun persists a posted run. The run_ref unique constraint makes
// this the idempotency backstop if two requests race past GetPayrollRunByRef.
func (r *Repository) RecordPayrollRun(ctx context.Context, in PayrollRunParams) (*PayrollRun, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO payroll_runs (run_ref, period, gross, paye, nssf, other_deductions, net, currency, status, journal_entry_id)
		VALUES ($1,$2,$3::numeric,$4::numeric,$5::numeric,$6::numeric,$7::numeric,$8,'posted',$9)
		RETURNING id, run_ref, period, gross::text, paye::text, nssf::text,
		          other_deductions::text, net::text, currency, status, journal_entry_id, created_at
	`, in.RunRef, in.Period, in.Gross, in.PAYE, in.NSSF, in.OtherDeductions, in.Net, in.Currency, in.JournalEntryID)
	var p PayrollRun
	if err := row.Scan(&p.ID, &p.RunRef, &p.Period, &p.Gross, &p.PAYE, &p.NSSF,
		&p.OtherDeductions, &p.Net, &p.Currency, &p.Status, &p.JournalEntryID, &p.CreatedAt); err != nil {
		return nil, err
	}
	return &p, nil
}

// ListPayrollRuns returns posted runs, newest first.
func (r *Repository) ListPayrollRuns(ctx context.Context, limit int) ([]PayrollRun, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, run_ref, period, gross::text, paye::text, nssf::text,
		       other_deductions::text, net::text, currency, status, journal_entry_id, created_at
		FROM payroll_runs ORDER BY created_at DESC LIMIT `+itoa(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PayrollRun{}
	for rows.Next() {
		var p PayrollRun
		if err := rows.Scan(&p.ID, &p.RunRef, &p.Period, &p.Gross, &p.PAYE, &p.NSSF,
			&p.OtherDeductions, &p.Net, &p.Currency, &p.Status, &p.JournalEntryID, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
