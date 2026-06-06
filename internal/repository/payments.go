package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
)

var (
	ErrOpenItemNotFound = errors.New("open item not found")
	ErrPaymentExceeds   = errors.New("payment exceeds open balance")
)

func (r *Repository) GetAROpenItem(ctx context.Context, id uuid.UUID) (*domain.AROpenItem, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, customer_ref, document_ref, description, amount::text, amount_paid::text, currency, due_date, status, journal_entry_id, source_event_id, created_at, updated_at
		FROM ar_open_items WHERE id = $1
	`, id)
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

func (r *Repository) GetAPOpenItem(ctx context.Context, id uuid.UUID) (*domain.APOpenItem, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, vendor_ref, document_ref, description, amount::text, amount_paid::text, currency, due_date, status, journal_entry_id, source_event_id, party_id, created_at, updated_at
		FROM ap_open_items WHERE id = $1
	`, id)
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

type ApplyPaymentParams struct {
	Direction      string
	OpenItemID     uuid.UUID
	Amount         decimal.Decimal
	Currency       string
	PaymentRef     string
	JournalEntryID uuid.UUID
}

// ApplyPayment records a payment, updates open-item amount_paid/status, and returns the payment row.
func (r *Repository) ApplyPayment(ctx context.Context, p ApplyPaymentParams) (*domain.Payment, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	table := "ar_open_items"
	if p.Direction == "ap" {
		table = "ap_open_items"
	}

	var totalStr, paidStr string
	err = tx.QueryRow(ctx, fmt.Sprintf(`
		SELECT amount::text, amount_paid::text FROM %s WHERE id = $1 FOR UPDATE
	`, table), p.OpenItemID).Scan(&totalStr, &paidStr)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrOpenItemNotFound
		}
		return nil, err
	}
	total, _ := decimal.NewFromString(totalStr)
	paid, _ := decimal.NewFromString(paidStr)
	newPaid := paid.Add(p.Amount)
	if newPaid.GreaterThan(total) {
		return nil, ErrPaymentExceeds
	}
	status := "partial"
	if newPaid.Equal(total) || newPaid.GreaterThan(total) {
		status = "closed"
	} else if newPaid.IsZero() {
		status = "open"
	}

	_, err = tx.Exec(ctx, fmt.Sprintf(`
		UPDATE %s SET amount_paid = $2, status = $3, updated_at = NOW()
		WHERE id = $1
	`, table), p.OpenItemID, newPaid, status)
	if err != nil {
		return nil, err
	}

	var payment domain.Payment
	err = tx.QueryRow(ctx, `
		INSERT INTO finance_payments (direction, open_item_id, amount, currency, payment_ref, journal_entry_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, direction, open_item_id, amount::text, currency, payment_ref, journal_entry_id, created_at
	`, p.Direction, p.OpenItemID, p.Amount, p.Currency, p.PaymentRef, p.JournalEntryID).Scan(
		&payment.ID, &payment.Direction, &payment.OpenItemID, &payment.Amount, &payment.Currency,
		&payment.PaymentRef, &payment.JournalEntryID, &payment.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &payment, nil
}

// PaymentWithJournalParams books a posted journal entry and applies a payment atomically.
type PaymentWithJournalParams struct {
	EventID, EventType, Source, CorrelationID, Description string
	Direction                                              string
	OpenItemID                                             uuid.UUID
	Amount                                                 decimal.Decimal
	Currency, PaymentRef                                   string
	Lines                                                  []ResolvedLine
}

// ApplyPaymentWithJournal records payment + GL in one transaction (idempotent on EventID).
func (r *Repository) ApplyPaymentWithJournal(ctx context.Context, p PaymentWithJournalParams) (*domain.Payment, error) {
	processed, err := r.IsEventProcessed(ctx, p.EventID)
	if err != nil {
		return nil, err
	}
	if processed {
		return r.paymentBySourceEvent(ctx, p.EventID)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var dup bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM processed_events WHERE event_id = $1)`, p.EventID).Scan(&dup); err != nil {
		return nil, err
	}
	if dup {
		if err := tx.Rollback(ctx); err != nil {
			return nil, err
		}
		return r.paymentBySourceEvent(ctx, p.EventID)
	}

	table := "ar_open_items"
	if p.Direction == "ap" {
		table = "ap_open_items"
	}

	var totalStr, paidStr string
	err = tx.QueryRow(ctx, fmt.Sprintf(`
		SELECT amount::text, amount_paid::text FROM %s WHERE id = $1 FOR UPDATE
	`, table), p.OpenItemID).Scan(&totalStr, &paidStr)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrOpenItemNotFound
		}
		return nil, err
	}
	total, _ := decimal.NewFromString(totalStr)
	paid, _ := decimal.NewFromString(paidStr)
	newPaid := paid.Add(p.Amount)
	if newPaid.GreaterThan(total) {
		return nil, ErrPaymentExceeds
	}
	status := "partial"
	if newPaid.Equal(total) || newPaid.GreaterThan(total) {
		status = "closed"
	} else if newPaid.IsZero() {
		status = "open"
	}

	var entryNumber string
	if err := tx.QueryRow(ctx, `
		SELECT 'JE-' || LPAD(nextval('journal_entry_number_seq')::text, 6, '0')
	`).Scan(&entryNumber); err != nil {
		return nil, err
	}

	postedAt := time.Now().UTC()
	var entryID uuid.UUID
	var corrID, srcEventID *string
	if p.CorrelationID != "" {
		corrID = &p.CorrelationID
	}
	if p.EventID != "" {
		srcEventID = &p.EventID
	}
	src := p.Source
	err = tx.QueryRow(ctx, `
		INSERT INTO journal_entries (
			entry_number, description, status, source_event_id, source_service, correlation_id, posted_at
		) VALUES ($1, $2, 'posted', $3, $4, $5, $6)
		RETURNING id
	`, entryNumber, p.Description, srcEventID, &src, corrID, postedAt).Scan(&entryID)
	if err != nil {
		return nil, err
	}

	for _, line := range p.Lines {
		_, err = tx.Exec(ctx, `
			INSERT INTO journal_lines (journal_entry_id, account_id, debit, credit, memo, line_order)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, entryID, line.AccountID, line.Debit, line.Credit, line.Memo, line.LineOrder)
		if err != nil {
			return nil, err
		}
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO processed_events (event_id, event_type) VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING
	`, p.EventID, p.EventType)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx, fmt.Sprintf(`
		UPDATE %s SET amount_paid = $2, status = $3, updated_at = NOW()
		WHERE id = $1
	`, table), p.OpenItemID, newPaid, status)
	if err != nil {
		return nil, err
	}

	var payment domain.Payment
	err = tx.QueryRow(ctx, `
		INSERT INTO finance_payments (direction, open_item_id, amount, currency, payment_ref, journal_entry_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, direction, open_item_id, amount::text, currency, payment_ref, journal_entry_id, created_at
	`, p.Direction, p.OpenItemID, p.Amount, p.Currency, p.PaymentRef, entryID).Scan(
		&payment.ID, &payment.Direction, &payment.OpenItemID, &payment.Amount, &payment.Currency,
		&payment.PaymentRef, &payment.JournalEntryID, &payment.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &payment, nil
}

func (r *Repository) paymentBySourceEvent(ctx context.Context, eventID string) (*domain.Payment, error) {
	entry, err := r.GetJournalEntryBySourceEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, fmt.Errorf("processed event %s missing journal entry", eventID)
	}
	return r.getPaymentByJournalEntryID(ctx, entry.ID)
}

func (r *Repository) getPaymentByJournalEntryID(ctx context.Context, journalEntryID uuid.UUID) (*domain.Payment, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, direction, open_item_id, amount::text, currency, payment_ref, journal_entry_id, created_at
		FROM finance_payments WHERE journal_entry_id = $1
	`, journalEntryID)
	var p domain.Payment
	if err := row.Scan(&p.ID, &p.Direction, &p.OpenItemID, &p.Amount, &p.Currency, &p.PaymentRef, &p.JournalEntryID, &p.CreatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("payment for journal %s not found", journalEntryID)
		}
		return nil, err
	}
	return &p, nil
}

func (r *Repository) ListPaymentsForItem(ctx context.Context, openItemID uuid.UUID) ([]domain.Payment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, direction, open_item_id, amount::text, currency, payment_ref, journal_entry_id, created_at
		FROM finance_payments
		WHERE open_item_id = $1
		ORDER BY created_at DESC
	`, openItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Payment
	for rows.Next() {
		var p domain.Payment
		if err := rows.Scan(&p.ID, &p.Direction, &p.OpenItemID, &p.Amount, &p.Currency, &p.PaymentRef, &p.JournalEntryID, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
