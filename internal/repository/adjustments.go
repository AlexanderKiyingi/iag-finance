package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
)

var (
	ErrAdjustmentConflict = errors.New("adjustment document already exists")
	ErrOriginalNotFound   = errors.New("original document not found")
	ErrAdjustmentTooLarge = errors.New("adjustment exceeds open balance")
)

type CreateAdjustmentParams struct {
	Kind                string
	Direction           string
	OriginalDocumentRef string
	DocumentRef         string
	PartyRef            string
	Amount              decimal.Decimal
	Currency            string
	Reason              string
	JournalEntryID      uuid.UUID
}

func (r *Repository) CreateAdjustment(ctx context.Context, p CreateAdjustmentParams) (*domain.Adjustment, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO finance_adjustments
			(kind, direction, original_document_ref, document_ref, party_ref, amount, currency, reason, journal_entry_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, kind, direction, original_document_ref, document_ref, party_ref, amount::text, currency, reason, status, journal_entry_id, created_at
	`, p.Kind, p.Direction, p.OriginalDocumentRef, p.DocumentRef, p.PartyRef, p.Amount.String(), p.Currency, p.Reason, p.JournalEntryID)

	var a domain.Adjustment
	if err := row.Scan(
		&a.ID, &a.Kind, &a.Direction, &a.OriginalDocumentRef, &a.DocumentRef, &a.PartyRef,
		&a.Amount, &a.Currency, &a.Reason, &a.Status, &a.JournalEntryID, &a.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *Repository) ListAdjustments(ctx context.Context, originalRef, direction string, limit int) ([]domain.Adjustment, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	query := `
		SELECT id, kind, direction, original_document_ref, document_ref, party_ref, amount::text, currency, reason, status, journal_entry_id, created_at
		FROM finance_adjustments WHERE 1=1`
	args := []any{}
	n := 1
	if originalRef != "" {
		query += ` AND original_document_ref = $` + itoa(n)
		args = append(args, originalRef)
		n++
	}
	if direction != "" {
		query += ` AND direction = $` + itoa(n)
		args = append(args, direction)
		n++
	}
	query += ` ORDER BY created_at DESC LIMIT $` + itoa(n)
	args = append(args, limit)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Adjustment
	for rows.Next() {
		var a domain.Adjustment
		if err := rows.Scan(
			&a.ID, &a.Kind, &a.Direction, &a.OriginalDocumentRef, &a.DocumentRef, &a.PartyRef,
			&a.Amount, &a.Currency, &a.Reason, &a.Status, &a.JournalEntryID, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// adjustOpenItemAmountTx applies a signed delta to an AR/AP open item amount
// inside tx, locking the row first. It refuses to drive the open balance below
// what has already been paid (or below zero) — the floor that stops a credit
// note from creating a negative receivable/payable. direction is "ar" or "ap".
func adjustOpenItemAmountTx(ctx context.Context, tx pgx.Tx, direction, documentRef string, delta decimal.Decimal) error {
	table := "ar_open_items"
	if direction == "ap" {
		table = "ap_open_items"
	}
	var amountStr, paidStr string
	err := tx.QueryRow(ctx, fmt.Sprintf(`
		SELECT amount::text, amount_paid::text FROM %s
		WHERE document_ref = $1 AND status != 'closed' FOR UPDATE
	`, table), documentRef).Scan(&amountStr, &paidStr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrOriginalNotFound
		}
		return err
	}
	amount, _ := decimal.NewFromString(amountStr)
	paid, _ := decimal.NewFromString(paidStr)
	newAmount := amount.Add(delta)
	if newAmount.IsNegative() || newAmount.LessThan(paid) {
		return ErrAdjustmentTooLarge
	}
	status := "open"
	switch {
	case newAmount.Equal(paid):
		status = "closed"
	case paid.IsPositive():
		status = "partial"
	}
	_, err = tx.Exec(ctx, fmt.Sprintf(`
		UPDATE %s SET amount = $2::numeric, status = $3, updated_at = NOW()
		WHERE document_ref = $1
	`, table), documentRef, newAmount.String(), status)
	return err
}

// insertAdjustmentTx writes a finance_adjustments row inside tx.
func insertAdjustmentTx(ctx context.Context, tx pgx.Tx, p CreateAdjustmentParams) (*domain.Adjustment, error) {
	row := tx.QueryRow(ctx, `
		INSERT INTO finance_adjustments
			(kind, direction, original_document_ref, document_ref, party_ref, amount, currency, reason, journal_entry_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, kind, direction, original_document_ref, document_ref, party_ref, amount::text, currency, reason, status, journal_entry_id, created_at
	`, p.Kind, p.Direction, p.OriginalDocumentRef, p.DocumentRef, p.PartyRef, p.Amount.String(), p.Currency, p.Reason, p.JournalEntryID)
	var a domain.Adjustment
	if err := row.Scan(
		&a.ID, &a.Kind, &a.Direction, &a.OriginalDocumentRef, &a.DocumentRef, &a.PartyRef,
		&a.Amount, &a.Currency, &a.Reason, &a.Status, &a.JournalEntryID, &a.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &a, nil
}

// BookAdjustmentParams describes a credit/debit note: the offsetting GL lines,
// the signed open-item delta, and the finance_adjustments row to record.
type BookAdjustmentParams struct {
	EventID, EventType string
	Description        string
	Source             string
	Direction          string // ar|ap
	Delta              decimal.Decimal
	FXRate             decimal.Decimal // original document rate → base (zero → 1)
	Lines              []ResolvedLine
	Adjustment         CreateAdjustmentParams
}

// BookAdjustment books the GL entry, applies the floored open-item delta, and
// records the finance_adjustments row — all atomically and idempotently on
// EventID, via the shared BookPostedEntry primitive. A replayed adjustment
// returns the one already recorded instead of double-posting.
func (r *Repository) BookAdjustment(ctx context.Context, p BookAdjustmentParams, postedAt time.Time, audit *AuditInfo) (*domain.Adjustment, error) {
	src := p.Source
	var out *domain.Adjustment
	entry, err := r.BookPostedEntry(ctx, CreateJournalParams{
		Description:   p.Description,
		SourceEventID: &p.EventID,
		SourceService: &src,
		Currency:      p.Adjustment.Currency,
		FXRate:        p.FXRate,
		Lines:         p.Lines,
	}, p.EventID, p.EventType, postedAt, func(ctx context.Context, tx pgx.Tx, entryID uuid.UUID) error {
		if err := adjustOpenItemAmountTx(ctx, tx, p.Direction, p.Adjustment.OriginalDocumentRef, p.Delta); err != nil {
			return err
		}
		adjParams := p.Adjustment
		adjParams.JournalEntryID = entryID
		a, err := insertAdjustmentTx(ctx, tx, adjParams)
		if err != nil {
			return err
		}
		out = a
		return nil
	}, audit)
	if err != nil {
		return nil, err
	}
	if out != nil {
		return out, nil
	}
	// Idempotent replay: the side-effect did not run because the entry already
	// existed. Return the adjustment linked to that entry.
	return r.getAdjustmentByJournalEntryID(ctx, entry.ID)
}

func (r *Repository) getAdjustmentByJournalEntryID(ctx context.Context, journalEntryID uuid.UUID) (*domain.Adjustment, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, kind, direction, original_document_ref, document_ref, party_ref, amount::text, currency, reason, status, journal_entry_id, created_at
		FROM finance_adjustments WHERE journal_entry_id = $1
	`, journalEntryID)
	var a domain.Adjustment
	if err := row.Scan(
		&a.ID, &a.Kind, &a.Direction, &a.OriginalDocumentRef, &a.DocumentRef, &a.PartyRef,
		&a.Amount, &a.Currency, &a.Reason, &a.Status, &a.JournalEntryID, &a.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &a, nil
}

// itoa renders a positional placeholder index. (Previously a hand-rolled
// version capped at "$4" and silently mis-bound a fifth argument.)
func itoa(n int) string { return strconv.Itoa(n) }

// AdjustARAmount applies a signed delta to an AR open item amount (credit negative, debit positive).
func (r *Repository) AdjustARAmount(ctx context.Context, documentRef string, delta decimal.Decimal) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE ar_open_items
		SET amount = amount + $2::numeric,
		    status = CASE
		        WHEN amount + $2::numeric <= amount_paid THEN 'closed'
		        WHEN amount_paid > 0 THEN 'partial'
		        ELSE status
		    END,
		    updated_at = NOW()
		WHERE document_ref = $1 AND status != 'closed'
	`, documentRef, delta.String())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrOriginalNotFound
	}
	return nil
}

// AdjustAPAmount applies a signed delta to an AP open item amount.
func (r *Repository) AdjustAPAmount(ctx context.Context, documentRef string, delta decimal.Decimal) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE ap_open_items
		SET amount = amount + $2::numeric,
		    status = CASE
		        WHEN amount + $2::numeric <= amount_paid THEN 'closed'
		        WHEN amount_paid > 0 THEN 'partial'
		        ELSE status
		    END,
		    updated_at = NOW()
		WHERE document_ref = $1 AND status != 'closed'
	`, documentRef, delta.String())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrOriginalNotFound
	}
	return nil
}

func (r *Repository) CreateAROpenItemWithBilling(ctx context.Context, customerRef, documentRef, description, amount, currency string, dueDate *time.Time, billingOrgID, billingIdentityID *uuid.UUID, outbox *OutboxEvent) (*domain.AROpenItem, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	fxRate := r.RateOrOne(ctx, currency, time.Now().UTC())
	var i domain.AROpenItem
	if err := tx.QueryRow(ctx, `
		INSERT INTO ar_open_items (customer_ref, document_ref, description, amount, currency, due_date, billing_org_id, billing_identity_id, fx_rate)
		VALUES ($1, $2, $3, $4::numeric, $5, $6, $7, $8, $9::numeric)
		RETURNING id, customer_ref, document_ref, description, amount::text, amount_paid::text, currency, due_date, status, journal_entry_id, source_event_id, created_at, updated_at
	`, customerRef, documentRef, description, amount, currency, dueDate, billingOrgID, billingIdentityID, fxRate.String()).Scan(
		&i.ID, &i.CustomerRef, &i.DocumentRef, &i.Description, &i.Amount, &i.AmountPaid,
		&i.Currency, &i.DueDate, &i.Status, &i.JournalEntryID, &i.SourceEventID,
		&i.CreatedAt, &i.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if outbox != nil {
		if err := enqueueOutboxTx(ctx, tx, *outbox); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &i, nil
}

func (r *Repository) ListARByCustomerRef(ctx context.Context, customerRef string, limit, offset int) ([]domain.AROpenItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, customer_ref, document_ref, description, amount::text, amount_paid::text, currency, due_date, status, journal_entry_id, source_event_id, created_at, updated_at
		FROM ar_open_items
		WHERE customer_ref = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, customerRef, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanARItems(rows)
}

func (r *Repository) GetAROpenItemByDocumentRef(ctx context.Context, documentRef string) (*domain.AROpenItem, error) {
	return r.GetARByDocumentRef(ctx, documentRef)
}

func (r *Repository) EnsurePaymentLinkToken(ctx context.Context, itemID uuid.UUID) (string, error) {
	var existing *string
	err := r.pool.QueryRow(ctx, `SELECT payment_link_token FROM ar_open_items WHERE id = $1`, itemID).Scan(&existing)
	if err == pgx.ErrNoRows {
		return "", ErrOpenItemNotFound
	}
	if err != nil {
		return "", err
	}
	if existing != nil && *existing != "" {
		return *existing, nil
	}
	token := uuid.New().String()
	_, err = r.pool.Exec(ctx, `
		UPDATE ar_open_items SET payment_link_token = $2, updated_at = NOW()
		WHERE id = $1 AND (payment_link_token IS NULL OR payment_link_token = '')
	`, itemID, token)
	if err != nil {
		return "", err
	}
	return token, nil
}
