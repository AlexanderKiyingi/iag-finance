package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type InvoiceLine struct {
	Description string `json:"description"`
	Quantity    string `json:"quantity"`
	UnitPrice   string `json:"unitPrice"`
	TaxCode     string `json:"taxCode,omitempty"`
	LineTotal   string `json:"lineTotal"`
	TaxAmount   string `json:"taxAmount"`
	LineOrder   int    `json:"lineOrder"`
}

type Invoice struct {
	ID          uuid.UUID     `json:"id"`
	Number      string        `json:"number"`
	CustomerRef string        `json:"customerRef"`
	Currency    string        `json:"currency"`
	IssueDate   *time.Time    `json:"issueDate,omitempty"`
	DueDate     *time.Time    `json:"dueDate,omitempty"`
	Status      string        `json:"status"`
	Subtotal    string        `json:"subtotal"`
	TaxTotal    string        `json:"taxTotal"`
	Total       string        `json:"total"`
	Notes       string        `json:"notes"`
	DocumentRef *string       `json:"documentRef,omitempty"`
	Lines       []InvoiceLine `json:"lines,omitempty"`
}

type InvoiceLineInput struct {
	Description string
	Quantity    decimal.Decimal
	UnitPrice   decimal.Decimal
	TaxCode     string
}

type CreateInvoiceInput struct {
	CustomerRef string
	Currency    string
	DueDate     *time.Time
	Notes       string
	Lines       []InvoiceLineInput
}

// CreateInvoice builds a draft invoice: each line total is qty × unit price, its
// tax is line total × the tax code's rate, and the header totals are the sums.
// Number is assigned from a sequence; entity comes from context.
func (r *Repository) CreateInvoice(ctx context.Context, in CreateInvoiceInput) (*Invoice, error) {
	currency := in.Currency
	if currency == "" {
		currency = r.baseCurrency
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var number string
	if err := tx.QueryRow(ctx, `SELECT 'INV-' || nextval('invoice_number_seq')::text`).Scan(&number); err != nil {
		return nil, err
	}

	subtotal, taxTotal := decimal.Zero, decimal.Zero
	type computed struct {
		in        InvoiceLineInput
		lineTotal decimal.Decimal
		taxAmount decimal.Decimal
	}
	var lines []computed
	for _, l := range in.Lines {
		lineTotal := l.Quantity.Mul(l.UnitPrice).Round(2)
		tax := decimal.Zero
		if l.TaxCode != "" {
			rate, ok, err := r.GetTaxRate(ctx, l.TaxCode)
			if err != nil {
				return nil, err
			}
			if ok {
				tax = lineTotal.Mul(rate).Round(2)
			}
		}
		subtotal = subtotal.Add(lineTotal)
		taxTotal = taxTotal.Add(tax)
		lines = append(lines, computed{l, lineTotal, tax})
	}
	total := subtotal.Add(taxTotal)

	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO invoices (entity_id, number, customer_ref, currency, due_date, status, subtotal, tax_total, total, notes)
		VALUES ($1, $2, $3, $4, $5, 'draft', $6, $7, $8, $9)
		RETURNING id
	`, EntityFromContext(ctx), number, in.CustomerRef, currency, in.DueDate,
		subtotal, taxTotal, total, in.Notes).Scan(&id); err != nil {
		return nil, err
	}
	for i, l := range lines {
		if _, err := tx.Exec(ctx, `
			INSERT INTO invoice_lines (invoice_id, description, quantity, unit_price, tax_code, line_total, tax_amount, line_order)
			VALUES ($1, $2, $3, $4, NULLIF($5,''), $6, $7, $8)
		`, id, l.in.Description, l.in.Quantity, l.in.UnitPrice, l.in.TaxCode, l.lineTotal, l.taxAmount, i); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetInvoice(ctx, id)
}

func (r *Repository) GetInvoice(ctx context.Context, id uuid.UUID) (*Invoice, error) {
	var inv Invoice
	err := r.pool.QueryRow(ctx, `
		SELECT id, number, customer_ref, currency, issue_date, due_date, status,
		       subtotal::text, tax_total::text, total::text, notes, document_ref
		FROM invoices WHERE id = $1
	`, id).Scan(&inv.ID, &inv.Number, &inv.CustomerRef, &inv.Currency, &inv.IssueDate, &inv.DueDate,
		&inv.Status, &inv.Subtotal, &inv.TaxTotal, &inv.Total, &inv.Notes, &inv.DocumentRef)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT description, quantity::text, unit_price::text, COALESCE(tax_code,''), line_total::text, tax_amount::text, line_order
		FROM invoice_lines WHERE invoice_id = $1 ORDER BY line_order
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var l InvoiceLine
		if err := rows.Scan(&l.Description, &l.Quantity, &l.UnitPrice, &l.TaxCode, &l.LineTotal, &l.TaxAmount, &l.LineOrder); err != nil {
			return nil, err
		}
		inv.Lines = append(inv.Lines, l)
	}
	return &inv, rows.Err()
}

func (r *Repository) ListInvoices(ctx context.Context, limit, offset int) ([]Invoice, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, number, customer_ref, currency, issue_date, due_date, status,
		       subtotal::text, tax_total::text, total::text, notes, document_ref
		FROM invoices WHERE entity_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3
	`, EntityFromContext(ctx), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invoice
	for rows.Next() {
		var inv Invoice
		if err := rows.Scan(&inv.ID, &inv.Number, &inv.CustomerRef, &inv.Currency, &inv.IssueDate, &inv.DueDate,
			&inv.Status, &inv.Subtotal, &inv.TaxTotal, &inv.Total, &inv.Notes, &inv.DocumentRef); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// MarkInvoiceIssued stamps a draft invoice issued and links it to its AR document.
func (r *Repository) MarkInvoiceIssued(ctx context.Context, id uuid.UUID, documentRef string, issueDate time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE invoices SET status = 'issued', document_ref = $2, issue_date = $3, updated_at = NOW()
		WHERE id = $1 AND status = 'draft'
	`, id, documentRef, issueDate)
	return err
}
