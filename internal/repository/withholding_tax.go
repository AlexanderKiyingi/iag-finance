package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// WHTReceipt records a withholding-tax certificate received from a customer.
type WHTReceipt struct {
	ID               string    `json:"id"`
	CertificateRef   string    `json:"certificateRef"`
	Customer         string    `json:"customer"`
	InvoiceReference string    `json:"invoiceReference"`
	TaxAuthority     string    `json:"taxAuthority"`
	Amount           string    `json:"amount"`
	ReceiptDate      string    `json:"receiptDate"`
	Currency         string    `json:"currency"`
	Notes            string    `json:"notes"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"createdAt"`
}

type CreateWHTReceiptInput struct {
	CertificateRef   string
	Customer         string
	InvoiceReference string
	TaxAuthority     string
	Amount           decimal.Decimal
	ReceiptDate      time.Time
	Currency         string
	Notes            string
}

// arControlCode ("1100") is declared in provisions_ecl.go (same package).
const whtRecoverableCode = "1150"

func (r *Repository) CreateWHTReceipt(ctx context.Context, in CreateWHTReceiptInput) (*WHTReceipt, error) {
	currency := in.Currency
	if currency == "" {
		currency = r.baseCurrency
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	ref := in.CertificateRef
	if ref == "" {
		var max *int
		_ = tx.QueryRow(ctx, `SELECT MAX(CAST(SUBSTRING(certificate_ref FROM 10) AS INT)) FROM wht_receipts WHERE certificate_ref LIKE 'WHT-2026-%'`).Scan(&max)
		n := 1
		if max != nil {
			n = *max + 1
		}
		ref = fmt.Sprintf("WHT-2026-%03d", n)
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO wht_receipts (certificate_ref, customer, invoice_reference, tax_authority, amount, receipt_date, currency, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id::text, certificate_ref, customer, invoice_reference, tax_authority, amount::text,
		          to_char(receipt_date,'YYYY-MM-DD'), currency, notes, status, created_at
	`, ref, in.Customer, in.InvoiceReference, in.TaxAuthority, in.Amount, in.ReceiptDate, currency, in.Notes)
	rec, err := scanWHTReceipt(row)
	if err != nil {
		return nil, err
	}

	// The withheld tax is a recoverable asset that settles part of the receivable:
	// Dr 1150 WHT Recoverable / Cr 1100 Accounts Receivable.
	if in.Amount.IsPositive() {
		whtID, err := accountIDByCodeTx(ctx, tx, whtRecoverableCode)
		if err != nil {
			return nil, err
		}
		arID, err := accountIDByCodeTx(ctx, tx, arControlCode)
		if err != nil {
			return nil, err
		}
		entryNumber, err := nextEntryNumberTx(ctx, tx)
		if err != nil {
			return nil, err
		}
		svc := "wht-receipt"
		if _, err := r.insertPostedEntryTx(ctx, tx, CreateJournalParams{
			EntryNumber:    entryNumber,
			Description:    "WHT certificate " + rec.CertificateRef,
			SourceService:  &svc,
			AccountingDate: in.ReceiptDate,
			Currency:       currency,
			Lines: []ResolvedLine{
				{AccountID: whtID, Debit: in.Amount, Memo: "WHT recoverable " + rec.CertificateRef, LineOrder: 0},
				{AccountID: arID, Credit: in.Amount, Memo: "Settle AR via WHT " + rec.InvoiceReference, LineOrder: 1},
			},
		}, in.ReceiptDate); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return rec, nil
}

func (r *Repository) ListWHTReceipts(ctx context.Context, limit, offset int) ([]WHTReceipt, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, certificate_ref, customer, invoice_reference, tax_authority, amount::text,
		       to_char(receipt_date,'YYYY-MM-DD'), currency, notes, status, created_at
		FROM wht_receipts ORDER BY receipt_date DESC, created_at DESC LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WHTReceipt
	for rows.Next() {
		rec, err := scanWHTReceipt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rec)
	}
	return out, rows.Err()
}

func scanWHTReceipt(s scannable) (*WHTReceipt, error) {
	var w WHTReceipt
	if err := s.Scan(&w.ID, &w.CertificateRef, &w.Customer, &w.InvoiceReference, &w.TaxAuthority,
		&w.Amount, &w.ReceiptDate, &w.Currency, &w.Notes, &w.Status, &w.CreatedAt); err != nil {
		return nil, err
	}
	return &w, nil
}
