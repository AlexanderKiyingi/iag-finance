package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// IAS 12 income taxes — current tax provision + deferred tax.

const (
	deferredTaxAssetCode = "1700"
	incomeTaxPayableCode = "2600"
	deferredTaxLiabCode  = "2610"
	incomeTaxExpenseCode = "5700"
	// UgandaCorporateRate is the default corporate income tax rate.
	UgandaCorporateRate = "0.30"
)

var (
	// ErrTaxRunExists indicates a current-tax run already exists for the period.
	ErrTaxRunExists = errors.New("income tax run already exists for period")
	// ErrDeferredTaxExists indicates a deferred-tax item already exists for the ref.
	ErrDeferredTaxExists = errors.New("deferred tax item already exists for reference")
)

// IncomeTaxRun is a current-tax provision for a period.
type IncomeTaxRun struct {
	ID            uuid.UUID       `json:"id"`
	Period        string          `json:"period"`
	TaxableProfit decimal.Decimal `json:"taxableProfit"`
	Rate          decimal.Decimal `json:"rate"`
	TaxAmount     decimal.Decimal `json:"taxAmount"`
	CreatedAt     time.Time       `json:"createdAt"`
}

// DeferredTaxItem is a recognised deferred tax asset or liability.
type DeferredTaxItem struct {
	ID             uuid.UUID       `json:"id"`
	Reference      string          `json:"reference"`
	Description    string          `json:"description"`
	TempDifference decimal.Decimal `json:"tempDifference"`
	DType          string          `json:"dtype"` // deductible | taxable
	Rate           decimal.Decimal `json:"rate"`
	TaxAmount      decimal.Decimal `json:"taxAmount"`
	CreatedAt      time.Time       `json:"createdAt"`
}

// RunCurrentTax books the current tax provision (Dr 5700 / Cr 2600) for a period
// on the supplied taxable profit at the given rate. Idempotent on
// incometax:<period>.
func (r *Repository) RunCurrentTax(ctx context.Context, period string, taxableProfit, rate decimal.Decimal, postedAt time.Time, audit *AuditInfo) (*IncomeTaxRun, error) {
	if period == "" {
		return nil, errors.New("period is required")
	}
	if rate.LessThanOrEqual(decimal.Zero) {
		return nil, errors.New("rate must be positive")
	}
	// A loss (or non-positive taxable profit) has no current tax charge.
	tax := decimal.Zero
	if taxableProfit.IsPositive() {
		tax = taxableProfit.Mul(rate).Round(2)
	}
	if tax.LessThanOrEqual(decimal.Zero) {
		return nil, errors.New("no current tax due on a non-positive taxable profit")
	}

	expenseID, err := r.accountIDByCode(ctx, incomeTaxExpenseCode)
	if err != nil {
		return nil, err
	}
	payableID, err := r.accountIDByCode(ctx, incomeTaxPayableCode)
	if err != nil {
		return nil, err
	}
	lines := []ResolvedLine{
		{AccountID: expenseID, Debit: tax, Memo: "Income tax expense " + period, LineOrder: 0},
		{AccountID: payableID, Credit: tax, Memo: "Income tax payable " + period, LineOrder: 1},
	}
	eventID := "incometax:" + period
	src := "iag.finance"

	var out *IncomeTaxRun
	_, err = r.BookPostedEntry(ctx, CreateJournalParams{
		Description:    "Current tax provision " + period,
		SourceEventID:  &eventID,
		SourceService:  &src,
		AccountingDate: postedAt,
		Lines:          lines,
	}, eventID, "finance.tax.current", postedAt, func(ctx context.Context, tx pgx.Tx, entryID uuid.UUID) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO income_tax_runs (entity_id, period, taxable_profit, rate, tax_amount, journal_entry_id)
			VALUES ($1, $2, $3::numeric, $4::numeric, $5::numeric, $6)
			RETURNING id
		`, EntityFromContext(ctx), period, taxableProfit.String(), rate.String(), tax.String(), entryID).Scan(new(uuid.UUID))
		if err != nil {
			if IsUniqueViolation(err) {
				return ErrTaxRunExists
			}
			return err
		}
		out = &IncomeTaxRun{Period: period, TaxableProfit: taxableProfit, Rate: rate, TaxAmount: tax}
		return nil
	}, audit)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return r.GetIncomeTaxRun(ctx, period)
	}
	return out, nil
}

// RecognizeDeferredTax books deferred tax on a temporary difference: a deductible
// difference raises a deferred tax asset (Dr 1700 / Cr 5700), a taxable
// difference a deferred tax liability (Dr 5700 / Cr 2610). Idempotent on
// deferredtax:<reference>.
func (r *Repository) RecognizeDeferredTax(ctx context.Context, ref, description string, tempDiff, rate decimal.Decimal, dtype string, postedAt time.Time, audit *AuditInfo) (*DeferredTaxItem, error) {
	if ref == "" {
		return nil, errors.New("reference is required")
	}
	if dtype != "deductible" && dtype != "taxable" {
		return nil, errors.New("dtype must be 'deductible' or 'taxable'")
	}
	if rate.LessThanOrEqual(decimal.Zero) {
		return nil, errors.New("rate must be positive")
	}
	if tempDiff.LessThanOrEqual(decimal.Zero) {
		return nil, errors.New("temporary difference must be positive")
	}
	tax := tempDiff.Mul(rate).Round(2)

	assetID, err := r.accountIDByCode(ctx, deferredTaxAssetCode)
	if err != nil {
		return nil, err
	}
	liabID, err := r.accountIDByCode(ctx, deferredTaxLiabCode)
	if err != nil {
		return nil, err
	}
	expenseID, err := r.accountIDByCode(ctx, incomeTaxExpenseCode)
	if err != nil {
		return nil, err
	}

	var lines []ResolvedLine
	if dtype == "deductible" {
		lines = []ResolvedLine{
			{AccountID: assetID, Debit: tax, Memo: "Deferred tax asset " + ref, LineOrder: 0},
			{AccountID: expenseID, Credit: tax, Memo: "Deferred tax credit " + ref, LineOrder: 1},
		}
	} else {
		lines = []ResolvedLine{
			{AccountID: expenseID, Debit: tax, Memo: "Deferred tax charge " + ref, LineOrder: 0},
			{AccountID: liabID, Credit: tax, Memo: "Deferred tax liability " + ref, LineOrder: 1},
		}
	}
	eventID := "deferredtax:" + ref
	src := "iag.finance"

	var out *DeferredTaxItem
	_, err = r.BookPostedEntry(ctx, CreateJournalParams{
		Description:    "Deferred tax " + ref,
		SourceEventID:  &eventID,
		SourceService:  &src,
		AccountingDate: postedAt,
		Lines:          lines,
	}, eventID, "finance.tax.deferred", postedAt, func(ctx context.Context, tx pgx.Tx, entryID uuid.UUID) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO deferred_tax_items (entity_id, reference, description, temp_difference, dtype, rate, tax_amount, journal_entry_id)
			VALUES ($1, $2, $3, $4::numeric, $5, $6::numeric, $7::numeric, $8)
			RETURNING id
		`, EntityFromContext(ctx), ref, description, tempDiff.String(), dtype, rate.String(), tax.String(), entryID).Scan(new(uuid.UUID))
		if err != nil {
			if IsUniqueViolation(err) {
				return ErrDeferredTaxExists
			}
			return err
		}
		out = &DeferredTaxItem{Reference: ref, Description: description, TempDifference: tempDiff, DType: dtype, Rate: rate, TaxAmount: tax}
		return nil
	}, audit)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return r.GetDeferredTaxItem(ctx, ref)
	}
	return out, nil
}

// GetIncomeTaxRun returns the current-tax run for a period.
func (r *Repository) GetIncomeTaxRun(ctx context.Context, period string) (*IncomeTaxRun, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, period, taxable_profit::text, rate::text, tax_amount::text, created_at
		FROM income_tax_runs WHERE period = $1
	`, period)
	var t IncomeTaxRun
	var tp, rate, tax string
	if err := row.Scan(&t.ID, &t.Period, &tp, &rate, &tax, &t.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTaxRunExists
		}
		return nil, err
	}
	t.TaxableProfit, _ = decimal.NewFromString(tp)
	t.Rate, _ = decimal.NewFromString(rate)
	t.TaxAmount, _ = decimal.NewFromString(tax)
	return &t, nil
}

// GetDeferredTaxItem returns the deferred-tax item for a reference.
func (r *Repository) GetDeferredTaxItem(ctx context.Context, ref string) (*DeferredTaxItem, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, reference, description, temp_difference::text, dtype, rate::text, tax_amount::text, created_at
		FROM deferred_tax_items WHERE reference = $1
	`, ref)
	var d DeferredTaxItem
	var td, rate, tax string
	if err := row.Scan(&d.ID, &d.Reference, &d.Description, &td, &d.DType, &rate, &tax, &d.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDeferredTaxExists
		}
		return nil, err
	}
	d.TempDifference, _ = decimal.NewFromString(td)
	d.Rate, _ = decimal.NewFromString(rate)
	d.TaxAmount, _ = decimal.NewFromString(tax)
	return &d, nil
}

// ListIncomeTaxRuns returns recent current-tax runs, newest first.
func (r *Repository) ListIncomeTaxRuns(ctx context.Context, limit int) ([]IncomeTaxRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, period, taxable_profit::text, rate::text, tax_amount::text, created_at
		FROM income_tax_runs ORDER BY created_at DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IncomeTaxRun
	for rows.Next() {
		var t IncomeTaxRun
		var tp, rate, tax string
		if err := rows.Scan(&t.ID, &t.Period, &tp, &rate, &tax, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.TaxableProfit, _ = decimal.NewFromString(tp)
		t.Rate, _ = decimal.NewFromString(rate)
		t.TaxAmount, _ = decimal.NewFromString(tax)
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListDeferredTaxItems returns recent deferred-tax items, newest first.
func (r *Repository) ListDeferredTaxItems(ctx context.Context, limit int) ([]DeferredTaxItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, reference, description, temp_difference::text, dtype, rate::text, tax_amount::text, created_at
		FROM deferred_tax_items ORDER BY created_at DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeferredTaxItem
	for rows.Next() {
		var d DeferredTaxItem
		var td, rate, tax string
		if err := rows.Scan(&d.ID, &d.Reference, &d.Description, &td, &d.DType, &rate, &tax, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.TempDifference, _ = decimal.NewFromString(td)
		d.Rate, _ = decimal.NewFromString(rate)
		d.TaxAmount, _ = decimal.NewFromString(tax)
		out = append(out, d)
	}
	return out, rows.Err()
}
