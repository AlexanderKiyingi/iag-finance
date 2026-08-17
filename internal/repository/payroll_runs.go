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
	ID              uuid.UUID `json:"id"`
	RunRef          string    `json:"runRef"`
	Period          string    `json:"period"`
	Gross           string    `json:"gross"`
	PAYE            string    `json:"paye"`
	NSSF            string    `json:"nssf"`
	OtherDeductions string    `json:"otherDeductions"`
	Net             string    `json:"net"`
	Currency        string    `json:"currency"`
	// EmployerNSSF is the employer's own contribution, booked as its own
	// expense/payable pair. Nil on runs posted before it was booked at all,
	// which is not the same as a run where it came to zero.
	EmployerNSSF   *string    `json:"employerNssf,omitempty"`
	Status         string     `json:"status"`
	JournalEntryID *uuid.UUID `json:"journalEntryId,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

// One column list and one reader for payroll_runs.
//
// The three queries below previously each carried their own hand-written scan
// of the same shape. Adding employer_nssf to the SELECTs and to only some of
// the scans left two of them reading thirteen columns into twelve destinations
// — and because Scan is variadic, nothing failed until it ran. One reader
// removes the possibility rather than relying on remembering.
const payrollRunColumns = `id, run_ref, period, gross::text, paye::text, nssf::text,
	other_deductions::text, net::text, currency, employer_nssf::text,
	status, journal_entry_id, created_at`

func scanPayrollRun(row pgx.Row) (*PayrollRun, error) {
	var p PayrollRun
	if err := row.Scan(&p.ID, &p.RunRef, &p.Period, &p.Gross, &p.PAYE, &p.NSSF,
		&p.OtherDeductions, &p.Net, &p.Currency, &p.EmployerNSSF,
		&p.Status, &p.JournalEntryID, &p.CreatedAt); err != nil {
		return nil, err
	}
	return &p, nil
}

// GetPayrollRunByRef returns the run with the given idempotency key, or nil if
// it has not been posted yet.
func (r *Repository) GetPayrollRunByRef(ctx context.Context, runRef string) (*PayrollRun, error) {
	p, err := scanPayrollRun(r.pool.QueryRow(ctx,
		`SELECT `+payrollRunColumns+` FROM payroll_runs WHERE run_ref = $1`, runRef))
	if err != nil {
		// Not posted yet is an answer, not a failure: the caller uses this to
		// decide whether to post.
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return p, nil
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
	// EmployerNSSF is written as NULL when empty, preserving the distinction
	// between "not booked" and "booked and came to nothing".
	EmployerNSSF   string
	JournalEntryID uuid.UUID
}

// RecordPayrollRun persists a posted run. The run_ref unique constraint makes
// this the idempotency backstop if two requests race past GetPayrollRunByRef.
func (r *Repository) RecordPayrollRun(ctx context.Context, in PayrollRunParams) (*PayrollRun, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO payroll_runs (run_ref, period, gross, paye, nssf, other_deductions, net, currency, employer_nssf, status, journal_entry_id)
		VALUES ($1,$2,$3::numeric,$4::numeric,$5::numeric,$6::numeric,$7::numeric,$8,NULLIF($9,'')::numeric,'posted',$10)
		RETURNING `+payrollRunColumns+`
	`, in.RunRef, in.Period, in.Gross, in.PAYE, in.NSSF, in.OtherDeductions, in.Net,
		in.Currency, in.EmployerNSSF, in.JournalEntryID)
	return scanPayrollRun(row)
}

// ListPayrollRuns returns posted runs, newest first.
func (r *Repository) ListPayrollRuns(ctx context.Context, limit int) ([]PayrollRun, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+payrollRunColumns+` FROM payroll_runs ORDER BY created_at DESC LIMIT `+itoa(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PayrollRun{}
	for rows.Next() {
		p, err := scanPayrollRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}
