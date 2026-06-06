package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/iag-finance/backend/internal/domain"
)

var ErrInvoiceNotFound = errors.New("invoice not found")

func (r *Repository) GetAPByDocumentRef(ctx context.Context, documentRef string) (*domain.APOpenItem, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, vendor_ref, document_ref, description, amount::text, amount_paid::text, currency, due_date, status, journal_entry_id, source_event_id, party_id, created_at, updated_at
		FROM ap_open_items WHERE document_ref = $1
	`, documentRef)
	var i domain.APOpenItem
	if err := row.Scan(
		&i.ID, &i.VendorRef, &i.DocumentRef, &i.Description, &i.Amount, &i.AmountPaid,
		&i.Currency, &i.DueDate, &i.Status, &i.JournalEntryID, &i.SourceEventID, &i.PartyID,
		&i.CreatedAt, &i.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &i, nil
}

func (r *Repository) GetARByDocumentRef(ctx context.Context, documentRef string) (*domain.AROpenItem, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, customer_ref, document_ref, description, amount::text, amount_paid::text, currency, due_date, status, journal_entry_id, source_event_id, created_at, updated_at
		FROM ar_open_items WHERE document_ref = $1
	`, documentRef)
	var i domain.AROpenItem
	if err := row.Scan(
		&i.ID, &i.CustomerRef, &i.DocumentRef, &i.Description, &i.Amount, &i.AmountPaid,
		&i.Currency, &i.DueDate, &i.Status, &i.JournalEntryID, &i.SourceEventID,
		&i.CreatedAt, &i.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &i, nil
}

func (r *Repository) ListAROpenItemsFiltered(ctx context.Context, status, q string, limit, offset int) ([]domain.AROpenItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	query := `
		SELECT id, customer_ref, document_ref, description, amount::text, amount_paid::text, currency, due_date, status, journal_entry_id, source_event_id, created_at, updated_at
		FROM ar_open_items WHERE 1=1`
	args := []any{}
	n := 1
	switch status {
	case "Overdue":
		query += ` AND status IN ('open', 'partial') AND due_date < CURRENT_DATE`
	case "Open":
		query += ` AND status = 'open'`
	case "Partial":
		query += ` AND status = 'partial'`
	case "Paid", "Closed":
		query += ` AND status = 'closed'`
	}
	if q != "" {
		query += fmt.Sprintf(` AND (document_ref ILIKE $%d OR customer_ref ILIKE $%d)`, n, n)
		args = append(args, "%"+q+"%")
		n++
	}
	query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, n, n+1)
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanARItems(rows)
}

func (r *Repository) UpdateAROpenItem(ctx context.Context, documentRef string, customerRef, description *string, dueDate *time.Time) (*domain.AROpenItem, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE ar_open_items SET
			customer_ref = COALESCE($2, customer_ref),
			description = COALESCE($3, description),
			due_date = COALESCE($4, due_date),
			updated_at = NOW()
		WHERE document_ref = $1 AND status != 'closed'
	`, documentRef, customerRef, description, dueDate)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrInvoiceNotFound
	}
	return r.GetARByDocumentRef(ctx, documentRef)
}

func (r *Repository) DeleteAROpenItem(ctx context.Context, documentRef string) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM ar_open_items
		WHERE document_ref = $1 AND status = 'open' AND amount_paid = 0
	`, documentRef)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrInvoiceNotFound
	}
	return nil
}

type SalesFunnel struct {
	Overdue FunnelBucket `json:"overdue"`
	Open    FunnelBucket `json:"open"`
	Paid    FunnelBucket `json:"paid"`
}

type FunnelBucket struct {
	Value float64 `json:"value"`
	Count int     `json:"count"`
}

func (r *Repository) SalesFunnel(ctx context.Context) (SalesFunnel, error) {
	var f SalesFunnel
	err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN status IN ('open','partial') AND due_date < CURRENT_DATE THEN amount - amount_paid ELSE 0 END), 0)::float8,
			COUNT(*) FILTER (WHERE status IN ('open','partial') AND due_date < CURRENT_DATE),
			COALESCE(SUM(CASE WHEN status IN ('open','partial') AND (due_date IS NULL OR due_date >= CURRENT_DATE) THEN amount - amount_paid ELSE 0 END), 0)::float8,
			COUNT(*) FILTER (WHERE status IN ('open','partial') AND (due_date IS NULL OR due_date >= CURRENT_DATE)),
			COALESCE(SUM(CASE WHEN status = 'closed' THEN amount ELSE 0 END), 0)::float8,
			COUNT(*) FILTER (WHERE status = 'closed')
		FROM ar_open_items
	`).Scan(
		&f.Overdue.Value, &f.Overdue.Count,
		&f.Open.Value, &f.Open.Count,
		&f.Paid.Value, &f.Paid.Count,
	)
	return f, err
}

type OverdueARItem struct {
	DocumentRef string
	CustomerRef string
	Amount      string
	Currency    string
	DueDate     time.Time
}

func (r *Repository) ListOverdueAR(ctx context.Context, cooldownHours int) ([]OverdueARItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT document_ref, customer_ref, (amount - amount_paid)::text, currency, due_date
		FROM ar_open_items
		WHERE status IN ('open', 'partial')
		  AND due_date < CURRENT_DATE
		  AND (overdue_notified_at IS NULL OR overdue_notified_at < NOW() - ($1 || ' hours')::interval)
		ORDER BY due_date
	`, cooldownHours)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OverdueARItem
	for rows.Next() {
		var it OverdueARItem
		if err := rows.Scan(&it.DocumentRef, &it.CustomerRef, &it.Amount, &it.Currency, &it.DueDate); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (r *Repository) MarkOverdueNotified(ctx context.Context, documentRefs []string) error {
	if len(documentRefs) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE ar_open_items SET overdue_notified_at = NOW()
		WHERE document_ref = ANY($1)
	`, documentRefs)
	return err
}
