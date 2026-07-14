package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// LateFee is an interest/penalty fee charged on an overdue invoice.
type LateFee struct {
	ID               string    `json:"id"`
	FeeRef           string    `json:"feeRef"`
	Customer         string    `json:"customer"`
	InvoiceReference string    `json:"invoiceReference"`
	Rate             string    `json:"rate"`
	Amount           string    `json:"amount"`
	FeeDate          string    `json:"feeDate"`
	Currency         string    `json:"currency"`
	Notes            string    `json:"notes"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"createdAt"`
}

type CreateLateFeeInput struct {
	FeeRef           string
	Customer         string
	InvoiceReference string
	Rate             decimal.Decimal
	Amount           decimal.Decimal
	FeeDate          time.Time
	Currency         string
	Notes            string
}

const lateFeeIncomeCode = "4300" // arControlCode "1100" is declared in provisions_ecl.go

func (r *Repository) CreateLateFee(ctx context.Context, in CreateLateFeeInput) (*LateFee, error) {
	currency := in.Currency
	if currency == "" {
		currency = r.baseCurrency
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	ref := in.FeeRef
	if ref == "" {
		var max *int
		_ = tx.QueryRow(ctx, `SELECT MAX(CAST(SUBSTRING(fee_ref FROM 9) AS INT)) FROM late_fees WHERE fee_ref LIKE 'LF-2026-%'`).Scan(&max)
		n := 1
		if max != nil {
			n = *max + 1
		}
		ref = fmt.Sprintf("LF-2026-%03d", n)
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO late_fees (fee_ref, customer, invoice_reference, rate, amount, fee_date, currency, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id::text, fee_ref, customer, invoice_reference, rate::text, amount::text,
		          to_char(fee_date,'YYYY-MM-DD'), currency, notes, status, created_at
	`, ref, in.Customer, in.InvoiceReference, in.Rate, in.Amount, in.FeeDate, currency, in.Notes)
	fee, err := scanLateFee(row)
	if err != nil {
		return nil, err
	}

	// A late fee increases the receivable and is finance income:
	// Dr 1100 Accounts Receivable / Cr 4300 Late Fee & Interest Income.
	arID, err := accountIDByCodeTx(ctx, tx, arControlCode)
	if err != nil {
		return nil, err
	}
	incomeID, err := accountIDByCodeTx(ctx, tx, lateFeeIncomeCode)
	if err != nil {
		return nil, err
	}
	entryNumber, err := nextEntryNumberTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	svc := "late-fee"
	if _, err := r.insertPostedEntryTx(ctx, tx, CreateJournalParams{
		EntryNumber:    entryNumber,
		Description:    "Late fee " + fee.FeeRef,
		SourceService:  &svc,
		AccountingDate: in.FeeDate,
		Currency:       currency,
		Lines: []ResolvedLine{
			{AccountID: arID, Debit: in.Amount, Memo: "Late fee on " + fee.InvoiceReference, LineOrder: 0},
			{AccountID: incomeID, Credit: in.Amount, Memo: "Late fee income " + fee.FeeRef, LineOrder: 1},
		},
	}, in.FeeDate); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return fee, nil
}

func (r *Repository) ListLateFees(ctx context.Context, limit, offset int) ([]LateFee, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, fee_ref, customer, invoice_reference, rate::text, amount::text,
		       to_char(fee_date,'YYYY-MM-DD'), currency, notes, status, created_at
		FROM late_fees ORDER BY fee_date DESC, created_at DESC LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LateFee
	for rows.Next() {
		fee, err := scanLateFee(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *fee)
	}
	return out, rows.Err()
}

func scanLateFee(s scannable) (*LateFee, error) {
	var f LateFee
	if err := s.Scan(&f.ID, &f.FeeRef, &f.Customer, &f.InvoiceReference, &f.Rate, &f.Amount,
		&f.FeeDate, &f.Currency, &f.Notes, &f.Status, &f.CreatedAt); err != nil {
		return nil, err
	}
	return &f, nil
}
