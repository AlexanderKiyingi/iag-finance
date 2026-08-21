package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// The subledger behind control account 1050 Payments Clearing.
//
// Every operational disbursement iag-payments settles lands in 1050 rather than
// being classified on arrival — finance cannot tell from a settlement whether
// the money paid a vendor invoice, a payroll run, a welfare loan or a claim, and
// guessing would either misclassify it or double-book against finance's own AP
// and payroll paths. The account is meant to be cleared against the originating
// document afterwards. Until this existed there was no list of what was in it.

// ErrClearingItemNotFound is returned when the referenced clearing row does not
// exist, or has already been cleared.
var ErrClearingItemNotFound = errors.New("payments clearing item not found or already cleared")

// PaymentsClearingItem is one settled disbursement awaiting classification.
type PaymentsClearingItem struct {
	ID              uuid.UUID  `json:"id"`
	InstructionID   string     `json:"instructionId"`
	ReferenceNumber string     `json:"referenceNumber,omitempty"`
	OriginService   string     `json:"originService,omitempty"`
	Category        string     `json:"category,omitempty"`
	PartyBusinessID string     `json:"partyBusinessId,omitempty"`
	Amount          string     `json:"amount"`
	Currency        string     `json:"currency"`
	SettledAt       time.Time  `json:"settledAt"`
	Status          string     `json:"status"`
	ClearedAgainst  string     `json:"clearedAgainst,omitempty"`
	ClearedAt       *time.Time `json:"clearedAt,omitempty"`
	// AgeDays is how long the disbursement has sat unclassified. The number
	// that matters: a clearing account is healthy when it turns over, not when
	// its balance happens to be small.
	AgeDays int `json:"ageDays"`
}

// PaymentsClearingInput is one settlement to record, taken from payments.settled.
type PaymentsClearingInput struct {
	InstructionID   string
	ReferenceNumber string
	OriginService   string
	Category        string
	PartyBusinessID string
	ProviderRef     string
	Amount          decimal.Decimal
	Currency        string
	SettledAt       time.Time
}

// AddPaymentsClearingItemTx records a settled disbursement inside the booking
// transaction, so the 1050 debit and the subledger row commit together or not at
// all. A GL balance whose subledger row failed to write is precisely the drift
// this account is supposed to make visible.
//
// Idempotent on instruction_id: a redelivered payments.settled updates the row
// it already wrote rather than adding a second one. It never reopens a cleared
// row — a redelivery arriving after an operator classified the payment must not
// undo that.
func AddPaymentsClearingItemTx(ctx context.Context, tx pgx.Tx, in PaymentsClearingInput) error {
	settled := in.SettledAt
	if settled.IsZero() {
		settled = time.Now().UTC()
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO payments_clearing_items (
			instruction_id, reference_number, origin_service, category,
			party_business_id, provider_ref, amount, currency, settled_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (instruction_id) DO UPDATE
		SET reference_number = EXCLUDED.reference_number,
		    origin_service   = EXCLUDED.origin_service,
		    category         = EXCLUDED.category,
		    party_business_id = EXCLUDED.party_business_id,
		    provider_ref     = EXCLUDED.provider_ref,
		    updated_at       = NOW()
		WHERE payments_clearing_items.status = 'open'
	`, in.InstructionID, in.ReferenceNumber, in.OriginService, in.Category,
		in.PartyBusinessID, in.ProviderRef, in.Amount, in.Currency, settled)
	return err
}

// ListPaymentsClearing returns clearing rows for the given entities, newest
// first. status "" lists everything; "open" is the useful default.
func (r *Repository) ListPaymentsClearing(ctx context.Context, entityIDs []uuid.UUID, status string, limit int) ([]PaymentsClearingItem, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, instruction_id, reference_number, origin_service, category,
		       party_business_id, amount::text, currency, settled_at, status,
		       COALESCE(cleared_against, ''), cleared_at,
		       GREATEST(0, EXTRACT(DAY FROM (NOW() - settled_at))::int)
		FROM payments_clearing_items
		WHERE entity_id = ANY($1::uuid[])
		  AND ($2::text = '' OR status = $2::text)
		ORDER BY settled_at DESC
		LIMIT $3`, entityIDs, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]PaymentsClearingItem, 0, 32)
	for rows.Next() {
		var it PaymentsClearingItem
		if err := rows.Scan(&it.ID, &it.InstructionID, &it.ReferenceNumber, &it.OriginService,
			&it.Category, &it.PartyBusinessID, &it.Amount, &it.Currency, &it.SettledAt,
			&it.Status, &it.ClearedAgainst, &it.ClearedAt, &it.AgeDays); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// ClearPaymentsClearingItem records which document a settled disbursement
// belonged to. It does not post a journal on its own — the reclassification out
// of 1050 is a ledger act with its own controls; this records the match that
// justifies it.
//
// Only an open row can be cleared, so a double submission is a no-op rather than
// a silent overwrite of the first classification.
func (r *Repository) ClearPaymentsClearingItem(ctx context.Context, id uuid.UUID, documentRef string, by *uuid.UUID) (*PaymentsClearingItem, error) {
	var it PaymentsClearingItem
	err := r.pool.QueryRow(ctx, `
		UPDATE payments_clearing_items
		SET status = 'cleared', cleared_against = $2, cleared_at = NOW(), cleared_by = $3, updated_at = NOW()
		WHERE id = $1 AND status = 'open'
		RETURNING id, instruction_id, reference_number, origin_service, category,
		          party_business_id, amount::text, currency, settled_at, status,
		          COALESCE(cleared_against, ''), cleared_at,
		          GREATEST(0, EXTRACT(DAY FROM (NOW() - settled_at))::int)
	`, id, documentRef, by).Scan(&it.ID, &it.InstructionID, &it.ReferenceNumber, &it.OriginService,
		&it.Category, &it.PartyBusinessID, &it.Amount, &it.Currency, &it.SettledAt,
		&it.Status, &it.ClearedAgainst, &it.ClearedAt, &it.AgeDays)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrClearingItemNotFound
	}
	if err != nil {
		return nil, err
	}
	return &it, nil
}

// openPaymentsClearingTotal sums the disbursements still awaiting a document —
// the subledger side of the 1050 control reconciliation.
func (r *Repository) openPaymentsClearingTotal(ctx context.Context, entityIDs []uuid.UUID) (decimal.Decimal, error) {
	var s string
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount), 0)::text FROM payments_clearing_items
		WHERE status = 'open' AND entity_id = ANY($1::uuid[])`, entityIDs).Scan(&s)
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromString(s)
}

// openGRNIAccrualTotal sums the goods received but not yet invoiced — the
// subledger side of the 2150 GR/IR control reconciliation. grni_accruals is
// not entity-scoped (a PO reference is unique platform-wide), so this is a
// whole-ledger figure and only ties out on a single-entity install.
func (r *Repository) openGRNIAccrualTotal(ctx context.Context) (decimal.Decimal, error) {
	var s string
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(accrued - cleared), 0)::text FROM grni_accruals`).Scan(&s)
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromString(s)
}
