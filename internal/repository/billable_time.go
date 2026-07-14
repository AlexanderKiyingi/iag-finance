package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// TimeEntry is an unbilled billable-time record (no GL until invoiced).
type TimeEntry struct {
	ID        string    `json:"id"`
	EntryRef  string    `json:"entryRef"`
	Customer  string    `json:"customer"`
	Employee  string    `json:"employee"`
	Project   string    `json:"project"`
	Hours     string    `json:"hours"`
	Rate      string    `json:"rate"`
	Amount    string    `json:"amount"`
	WorkDate  string    `json:"workDate"`
	Currency  string    `json:"currency"`
	Notes     string    `json:"notes"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

type CreateTimeEntryInput struct {
	EntryRef string
	Customer string
	Employee string
	Project  string
	Hours    decimal.Decimal
	Rate     decimal.Decimal
	Amount   decimal.Decimal
	WorkDate time.Time
	Currency string
	Notes    string
}

func (r *Repository) CreateTimeEntry(ctx context.Context, in CreateTimeEntryInput) (*TimeEntry, error) {
	currency := in.Currency
	if currency == "" {
		currency = r.baseCurrency
	}
	ref := in.EntryRef
	if ref == "" {
		var max *int
		_ = r.pool.QueryRow(ctx, `SELECT MAX(CAST(SUBSTRING(entry_ref FROM 9) AS INT)) FROM billable_time_entries WHERE entry_ref LIKE 'BT-2026-%'`).Scan(&max)
		n := 1
		if max != nil {
			n = *max + 1
		}
		ref = fmt.Sprintf("BT-2026-%03d", n)
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO billable_time_entries (entry_ref, customer, employee, project, hours, rate, amount, work_date, currency, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id::text, entry_ref, customer, employee, project, hours::text, rate::text, amount::text,
		          to_char(work_date,'YYYY-MM-DD'), currency, notes, status, created_at
	`, ref, in.Customer, in.Employee, in.Project, in.Hours, in.Rate, in.Amount, in.WorkDate, currency, in.Notes)
	return scanTimeEntry(row)
}

func (r *Repository) ListTimeEntries(ctx context.Context, limit, offset int) ([]TimeEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, entry_ref, customer, employee, project, hours::text, rate::text, amount::text,
		       to_char(work_date,'YYYY-MM-DD'), currency, notes, status, created_at
		FROM billable_time_entries ORDER BY work_date DESC, created_at DESC LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TimeEntry
	for rows.Next() {
		e, err := scanTimeEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func scanTimeEntry(s scannable) (*TimeEntry, error) {
	var e TimeEntry
	if err := s.Scan(&e.ID, &e.EntryRef, &e.Customer, &e.Employee, &e.Project, &e.Hours, &e.Rate,
		&e.Amount, &e.WorkDate, &e.Currency, &e.Notes, &e.Status, &e.CreatedAt); err != nil {
		return nil, err
	}
	return &e, nil
}
