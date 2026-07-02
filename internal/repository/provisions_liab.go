package repository

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// IAS 37 provisions and IAS 16/37 decommissioning obligations.
//
// A general provision expenses on recognition (Dr 5500 / Cr 2400); a
// decommissioning provision capitalises into the asset (Dr 1500 / Cr 2410).
// Discount unwinding is a finance cost (Dr 5510 / Cr liability). Utilisation
// settles the liability against cash; remeasurement adjusts it; reversal releases
// it back to the expense.

const (
	provisionCode      = "2400"
	decommissionCode   = "2410"
	provisionExpense   = "5500"
	unwindFinanceCost  = "5510"
)

// ErrProvisionNotFound indicates the provision does not exist.
var ErrProvisionNotFound = errors.New("provision not found")

// ErrProvisionClosed indicates the provision is already settled/reversed.
var ErrProvisionClosed = errors.New("provision is not active")

// LiabProvision is one row of the provision register.
type LiabProvision struct {
	ID                 uuid.UUID       `json:"id"`
	Kind               string          `json:"kind"`
	Description        string          `json:"description"`
	Estimate           decimal.Decimal `json:"estimate"`
	DiscountRate       decimal.Decimal `json:"discountRate"`
	ExpectedSettlement *string         `json:"expectedSettlement,omitempty"`
	CarryingAmount     decimal.Decimal `json:"carryingAmount"`
	Currency           string          `json:"currency"`
	AssetRef           *string         `json:"assetRef,omitempty"`
	Status             string          `json:"status"`
	CreatedAt          time.Time       `json:"createdAt"`
}

// RecognizeProvisionInput describes a new provision.
type RecognizeProvisionInput struct {
	Kind               string
	Description        string
	Estimate           decimal.Decimal
	DiscountRate       decimal.Decimal
	ExpectedSettlement *time.Time
	Currency           string
	AssetRef           string
}

// liabilityCode / recognitionDebit return the GL accounts for a provision kind.
func liabilityCode(kind string) string {
	if kind == "decommissioning" {
		return decommissionCode
	}
	return provisionCode
}
func recognitionDebit(kind string) string {
	if kind == "decommissioning" {
		return fixedAssetsCode // capitalise into the asset
	}
	return provisionExpense
}

// RecognizeProvision books the initial provision (discounting to present value
// when a discount rate and settlement date are given) and registers it.
func (r *Repository) RecognizeProvision(ctx context.Context, in RecognizeProvisionInput, postedAt time.Time, audit *AuditInfo) (*LiabProvision, error) {
	if in.Estimate.LessThanOrEqual(decimal.Zero) {
		return nil, errors.New("estimate must be positive")
	}
	if in.Kind == "" {
		in.Kind = "general"
	}
	if in.Currency == "" {
		in.Currency = r.baseCurrency
	}
	carrying := presentValue(in.Estimate, in.DiscountRate, in.ExpectedSettlement, postedAt)

	debitID, err := r.accountIDByCode(ctx, recognitionDebit(in.Kind))
	if err != nil {
		return nil, err
	}
	liabID, err := r.accountIDByCode(ctx, liabilityCode(in.Kind))
	if err != nil {
		return nil, err
	}
	lines := []ResolvedLine{
		{AccountID: debitID, Debit: carrying, Memo: "Recognise provision", LineOrder: 0},
		{AccountID: liabID, Credit: carrying, Memo: "Provision liability", LineOrder: 1},
	}
	provID := uuid.New()
	eventID := "provision.recognize:" + provID.String()
	src := "iag.finance"
	var out *LiabProvision
	_, err = r.BookPostedEntry(ctx, CreateJournalParams{
		Description:    "Provision: " + in.Description,
		SourceEventID:  &eventID,
		SourceService:  &src,
		Currency:       in.Currency,
		AccountingDate: postedAt,
		Lines:          lines,
	}, eventID, "finance.provision.recognize", postedAt, func(ctx context.Context, tx pgx.Tx, entryID uuid.UUID) error {
		var assetRef *string
		if in.AssetRef != "" {
			assetRef = &in.AssetRef
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO liab_provisions (id, kind, description, estimate, discount_rate, expected_settlement, carrying_amount, currency, asset_ref, entity_id)
			VALUES ($1, $2, $3, $4::numeric, $5::numeric, $6, $7::numeric, $8, $9, $10)
		`, provID, in.Kind, in.Description, in.Estimate.String(), in.DiscountRate.String(), in.ExpectedSettlement, carrying.String(), in.Currency, assetRef, EntityFromContext(ctx)); err != nil {
			return err
		}
		return insertProvisionMovement(ctx, tx, provID, postedAt, "recognize", carrying, entryID)
	}, audit)
	if err != nil {
		return nil, err
	}
	out, err = r.GetProvision(ctx, provID)
	return out, err
}

// UnwindProvisionDiscount accrues one period's unwind of the discount as a
// finance cost, increasing the carrying amount toward the undiscounted estimate.
func (r *Repository) UnwindProvisionDiscount(ctx context.Context, id uuid.UUID, reference string, postedAt time.Time, audit *AuditInfo) (*LiabProvision, error) {
	p, err := r.GetProvision(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Status != "active" {
		return nil, ErrProvisionClosed
	}
	interest := p.CarryingAmount.Mul(p.DiscountRate).Round(2)
	if interest.LessThanOrEqual(decimal.Zero) {
		return p, nil // nothing to unwind
	}
	costID, err := r.accountIDByCode(ctx, unwindFinanceCost)
	if err != nil {
		return nil, err
	}
	liabID, err := r.accountIDByCode(ctx, liabilityCode(p.Kind))
	if err != nil {
		return nil, err
	}
	lines := []ResolvedLine{
		{AccountID: costID, Debit: interest, Memo: "Unwind discount", LineOrder: 0},
		{AccountID: liabID, Credit: interest, Memo: "Provision unwind", LineOrder: 1},
	}
	return r.bookProvisionOp(ctx, id, "unwind", interest, "provision.unwind:"+id.String()+":"+refOrDate(reference, postedAt),
		"finance.provision.unwind", "Unwind provision discount", p.Currency, lines, postedAt, audit)
}

// UtilizeProvision settles part of a provision against cash.
func (r *Repository) UtilizeProvision(ctx context.Context, id uuid.UUID, amount decimal.Decimal, reference string, postedAt time.Time, audit *AuditInfo) (*LiabProvision, error) {
	p, err := r.GetProvision(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Status != "active" {
		return nil, ErrProvisionClosed
	}
	if amount.GreaterThan(p.CarryingAmount) {
		amount = p.CarryingAmount
	}
	if amount.LessThanOrEqual(decimal.Zero) {
		return p, nil
	}
	liabID, err := r.accountIDByCode(ctx, liabilityCode(p.Kind))
	if err != nil {
		return nil, err
	}
	cashID, err := r.accountIDByCode(ctx, cashCode)
	if err != nil {
		return nil, err
	}
	lines := []ResolvedLine{
		{AccountID: liabID, Debit: amount, Memo: "Utilise provision", LineOrder: 0},
		{AccountID: cashID, Credit: amount, Memo: "Provision settled", LineOrder: 1},
	}
	return r.bookProvisionOp(ctx, id, "utilize", amount.Neg(), "provision.utilize:"+id.String()+":"+refOrDate(reference, postedAt),
		"finance.provision.utilize", "Utilise provision", p.Currency, lines, postedAt, audit)
}

// RemeasureProvision adjusts the carrying amount to a new estimate.
func (r *Repository) RemeasureProvision(ctx context.Context, id uuid.UUID, newEstimate decimal.Decimal, reference string, postedAt time.Time, audit *AuditInfo) (*LiabProvision, error) {
	p, err := r.GetProvision(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Status != "active" {
		return nil, ErrProvisionClosed
	}
	delta := newEstimate.Sub(p.CarryingAmount)
	if delta.IsZero() {
		return p, nil
	}
	debitCode := recognitionDebit(p.Kind)
	liab := liabilityCode(p.Kind)
	debitID, err := r.accountIDByCode(ctx, debitCode)
	if err != nil {
		return nil, err
	}
	liabID, err := r.accountIDByCode(ctx, liab)
	if err != nil {
		return nil, err
	}
	var lines []ResolvedLine
	if delta.IsPositive() {
		lines = []ResolvedLine{
			{AccountID: debitID, Debit: delta, Memo: "Remeasure provision (increase)", LineOrder: 0},
			{AccountID: liabID, Credit: delta, Memo: "Provision increase", LineOrder: 1},
		}
	} else {
		amt := delta.Neg()
		lines = []ResolvedLine{
			{AccountID: liabID, Debit: amt, Memo: "Remeasure provision (decrease)", LineOrder: 0},
			{AccountID: debitID, Credit: amt, Memo: "Provision decrease", LineOrder: 1},
		}
	}
	return r.bookProvisionOp(ctx, id, "remeasure", delta, "provision.remeasure:"+id.String()+":"+refOrDate(reference, postedAt),
		"finance.provision.remeasure", "Remeasure provision", p.Currency, lines, postedAt, audit)
}

// ReverseProvision releases an unused provision back to the expense.
func (r *Repository) ReverseProvision(ctx context.Context, id uuid.UUID, reference string, postedAt time.Time, audit *AuditInfo) (*LiabProvision, error) {
	p, err := r.GetProvision(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Status != "active" {
		return nil, ErrProvisionClosed
	}
	if p.CarryingAmount.LessThanOrEqual(decimal.Zero) {
		return p, nil
	}
	liabID, err := r.accountIDByCode(ctx, liabilityCode(p.Kind))
	if err != nil {
		return nil, err
	}
	expenseID, err := r.accountIDByCode(ctx, provisionExpense)
	if err != nil {
		return nil, err
	}
	lines := []ResolvedLine{
		{AccountID: liabID, Debit: p.CarryingAmount, Memo: "Reverse provision", LineOrder: 0},
		{AccountID: expenseID, Credit: p.CarryingAmount, Memo: "Provision reversal", LineOrder: 1},
	}
	return r.bookProvisionOp(ctx, id, "reverse", p.CarryingAmount.Neg(), "provision.reverse:"+id.String()+":"+refOrDate(reference, postedAt),
		"finance.provision.reverse", "Reverse provision", p.Currency, lines, postedAt, audit)
}

// bookProvisionOp books the GL entry for a movement, records the movement, and
// updates the carrying amount/status, atomically and idempotently on eventID.
func (r *Repository) bookProvisionOp(ctx context.Context, id uuid.UUID, movementKind string, carryingDelta decimal.Decimal, eventID, eventType, description, currency string, lines []ResolvedLine, postedAt time.Time, audit *AuditInfo) (*LiabProvision, error) {
	src := "iag.finance"
	_, err := r.BookPostedEntry(ctx, CreateJournalParams{
		Description:    description,
		SourceEventID:  &eventID,
		SourceService:  &src,
		Currency:       currency,
		AccountingDate: postedAt,
		Lines:          lines,
	}, eventID, eventType, postedAt, func(ctx context.Context, tx pgx.Tx, entryID uuid.UUID) error {
		if _, err := tx.Exec(ctx, `
			UPDATE liab_provisions
			SET carrying_amount = GREATEST(carrying_amount + $2::numeric, 0),
			    status = CASE
			        WHEN $3 = 'reverse' THEN 'reversed'
			        WHEN carrying_amount + $2::numeric <= 0 THEN 'settled'
			        ELSE status END,
			    updated_at = NOW()
			WHERE id = $1
		`, id, carryingDelta.String(), movementKind); err != nil {
			return err
		}
		return insertProvisionMovement(ctx, tx, id, postedAt, movementKind, carryingDelta, entryID)
	}, audit)
	if err != nil {
		return nil, err
	}
	return r.GetProvision(ctx, id)
}

func insertProvisionMovement(ctx context.Context, tx pgx.Tx, provID uuid.UUID, effective time.Time, kind string, amount decimal.Decimal, entryID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO provision_movements (provision_id, effective_date, kind, amount, journal_entry_id)
		VALUES ($1, $2, $3, $4::numeric, $5)
	`, provID, effective, kind, amount.String(), entryID)
	return err
}

// GetProvision returns one provision by id.
func (r *Repository) GetProvision(ctx context.Context, id uuid.UUID) (*LiabProvision, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, kind, description, estimate::text, discount_rate::text, to_char(expected_settlement,'YYYY-MM-DD'),
		       carrying_amount::text, currency, asset_ref, status, created_at
		FROM liab_provisions WHERE id = $1
	`, id)
	return scanProvision(row)
}

// ListProvisions returns recent provisions, newest first.
func (r *Repository) ListProvisions(ctx context.Context, limit int) ([]LiabProvision, error) {
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, kind, description, estimate::text, discount_rate::text, to_char(expected_settlement,'YYYY-MM-DD'),
		       carrying_amount::text, currency, asset_ref, status, created_at
		FROM liab_provisions ORDER BY created_at DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LiabProvision
	for rows.Next() {
		p, err := scanProvision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func scanProvision(row scannable) (*LiabProvision, error) {
	var p LiabProvision
	var estimate, rate, carrying string
	if err := row.Scan(&p.ID, &p.Kind, &p.Description, &estimate, &rate, &p.ExpectedSettlement, &carrying, &p.Currency, &p.AssetRef, &p.Status, &p.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProvisionNotFound
		}
		return nil, err
	}
	p.Estimate, _ = decimal.NewFromString(estimate)
	p.DiscountRate, _ = decimal.NewFromString(rate)
	p.CarryingAmount, _ = decimal.NewFromString(carrying)
	return &p, nil
}

// presentValue discounts estimate to today when a rate and future settlement are
// given; otherwise returns the undiscounted estimate.
func presentValue(estimate, rate decimal.Decimal, settlement *time.Time, now time.Time) decimal.Decimal {
	if settlement == nil || rate.LessThanOrEqual(decimal.Zero) {
		return estimate
	}
	years := settlement.Sub(now).Hours() / (24 * 365)
	if years <= 0 {
		return estimate
	}
	rateF, _ := rate.Float64()
	estF, _ := estimate.Float64()
	pv := estF / math.Pow(1+rateF, years)
	return decimal.NewFromFloat(pv).Round(2)
}

// refOrDate returns the reference if set, else the posting date (day precision),
// so an operation is idempotent per reference or per day.
func refOrDate(reference string, postedAt time.Time) string {
	if reference != "" {
		return reference
	}
	return postedAt.Format("2006-01-02")
}
