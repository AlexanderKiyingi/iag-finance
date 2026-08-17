package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type RecurringInvoice struct {
	ID          uuid.UUID       `json:"id"`
	EntityID    uuid.UUID       `json:"entityId"`
	CustomerRef string          `json:"customerRef"`
	Currency    string          `json:"currency"`
	Cadence     string          `json:"cadence"`
	NextRun     time.Time       `json:"nextRun"`
	Template    json.RawMessage `json:"template"`
	Notes       string          `json:"notes"`
	Active      bool            `json:"active"`
	// Optional IFRS 15 recognition inherited by each generated invoice: 'ratable'
	// spreads that invoice's revenue over RecognitionPeriods months from its issue.
	RecognitionMethod  string `json:"recognitionMethod,omitempty"`
	RecognitionPeriods int    `json:"recognitionPeriods,omitempty"`
}

type CreateRecurringInput struct {
	CustomerRef string
	Currency    string
	Cadence     string
	NextRun     time.Time
	Template    json.RawMessage
	Notes       string
	// Optional IFRS 15 recognition (see RecurringInvoice).
	RecognitionMethod  string
	RecognitionPeriods int
}

func (r *Repository) CreateRecurringInvoice(ctx context.Context, in CreateRecurringInput) (*RecurringInvoice, error) {
	currency := in.Currency
	if currency == "" {
		currency = r.baseCurrency
	}
	var ri RecurringInvoice
	err := r.pool.QueryRow(ctx, `
		INSERT INTO recurring_invoices (entity_id, customer_ref, currency, cadence, next_run, template, notes, recognition_method, recognition_periods)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, entity_id, customer_ref, currency, cadence, next_run, template, notes, active, recognition_method, recognition_periods
	`, EntityFromContext(ctx), in.CustomerRef, currency, in.Cadence, in.NextRun, in.Template, in.Notes, in.RecognitionMethod, in.RecognitionPeriods).Scan(
		&ri.ID, &ri.EntityID, &ri.CustomerRef, &ri.Currency, &ri.Cadence, &ri.NextRun, &ri.Template, &ri.Notes, &ri.Active, &ri.RecognitionMethod, &ri.RecognitionPeriods)
	if err != nil {
		return nil, err
	}
	return &ri, nil
}

func (r *Repository) ListRecurringInvoices(ctx context.Context) ([]RecurringInvoice, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, entity_id, customer_ref, currency, cadence, next_run, template, notes, active, recognition_method, recognition_periods
		FROM recurring_invoices WHERE entity_id = $1 ORDER BY created_at DESC
	`, EntityFromContext(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecurring(rows)
}

// ListDueRecurring returns active schedules due on/before asOf, across entities
// (the worker runs globally and sets each invoice's entity from the schedule).
func (r *Repository) ListDueRecurring(ctx context.Context, asOf time.Time) ([]RecurringInvoice, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, entity_id, customer_ref, currency, cadence, next_run, template, notes, active, recognition_method, recognition_periods
		FROM recurring_invoices WHERE active AND next_run <= $1 ORDER BY next_run
	`, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecurring(rows)
}

func scanRecurring(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]RecurringInvoice, error) {
	var out []RecurringInvoice
	for rows.Next() {
		var ri RecurringInvoice
		if err := rows.Scan(&ri.ID, &ri.EntityID, &ri.CustomerRef, &ri.Currency, &ri.Cadence, &ri.NextRun, &ri.Template, &ri.Notes, &ri.Active, &ri.RecognitionMethod, &ri.RecognitionPeriods); err != nil {
			return nil, err
		}
		out = append(out, ri)
	}
	return out, rows.Err()
}

// ClaimRecurring advances a schedule's next_run, but only while it still holds
// the value the caller read. It reports false when it did not — meaning another
// instance of this service claimed the same period first.
//
// This is what makes the recurring invoicer safe to run on more than one
// replica. An unconditional UPDATE would let two workers read the same due
// schedule, both generate an invoice, and both advance the date; the customer
// gets billed twice and the ledger carries two postings. A conditional update
// is atomic in Postgres, so exactly one caller sees a row affected.
func (r *Repository) ClaimRecurring(ctx context.Context, id uuid.UUID, from, next time.Time) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE recurring_invoices SET next_run = $3 WHERE id = $1 AND next_run = $2`,
		id, from, next)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
