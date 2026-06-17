package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/iag-finance/backend/internal/chainaudit"
	"github.com/iag-finance/backend/internal/domain"
)

// AuditInfo, when supplied to a booking call, appends a tamper-evident audit
// chain entry inside the same transaction as the GL mutation, so the audit
// record and the ledger change commit (or roll back) together.
type AuditInfo struct {
	Actor     string
	EventType string
	Message   string
}

// appendAudit chains an audit entry within tx when audit is set and attributed.
func appendAudit(ctx context.Context, tx pgx.Tx, audit *AuditInfo) error {
	if audit == nil || audit.Actor == "" {
		return nil
	}
	_, err := chainaudit.AppendChainTx(ctx, tx, audit.Actor, audit.EventType, audit.Message)
	return err
}

// IsUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505). Used to turn a racing duplicate insert into an
// idempotent "already booked" outcome instead of a hard error — and by event
// consumers to detect a duplicate intake without brittle error-string matching.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isUniqueViolation(err error) bool { return IsUniqueViolation(err) }

// nextEntryNumberTx allocates the next JE-NNNNNN number inside the given tx so
// the number, the entry, and its side-effects share one transaction boundary.
func nextEntryNumberTx(ctx context.Context, tx pgx.Tx) (string, error) {
	var n string
	err := tx.QueryRow(ctx, `
		SELECT 'JE-' || LPAD(nextval('journal_entry_number_seq')::text, 6, '0')
	`).Scan(&n)
	return n, err
}

// insertPostedEntryTx inserts an already-posted journal entry plus its lines
// inside tx and returns the new entry id. Each line stores its transaction
// currency and a base-currency equivalent (nominal × the entry FX rate); since
// all lines of an entry share one rate, base debits == base credits whenever
// nominal does, so the balanced-entry constraint trigger (migration 018) still
// guarantees a balanced entry — in base currency too.
func (r *Repository) insertPostedEntryTx(ctx context.Context, tx pgx.Tx, p CreateJournalParams, postedAt time.Time) (uuid.UUID, error) {
	var entryID uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO journal_entries (
			entry_number, description, status, source_event_id, source_service, correlation_id, created_by, posted_at, accounting_date, entity_id
		) VALUES ($1, $2, 'posted', $3, $4, $5, $6, $7, $8, $9)
		RETURNING id
	`, p.EntryNumber, p.Description, p.SourceEventID, p.SourceService, p.CorrelationID, p.CreatedBy, postedAt, resolveAccountingDate(p.AccountingDate, postedAt), EntityFromContext(ctx)).Scan(&entryID)
	if err != nil {
		return uuid.Nil, err
	}
	entryCurrency := p.lineCurrency(r.baseCurrency)
	rate := p.rate()
	for _, line := range p.Lines {
		debitBase, creditBase := line.baseAmounts(rate)
		currency := line.currencyOr(entryCurrency)
		if _, err := tx.Exec(ctx, `
			INSERT INTO journal_lines (journal_entry_id, account_id, debit, credit, memo, line_order, currency, debit_base, credit_base, cost_center_id, project_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`, entryID, line.AccountID, line.Debit, line.Credit, line.Memo, line.LineOrder, currency, debitBase, creditBase, line.CostCenterID, line.ProjectID); err != nil {
			return uuid.Nil, err
		}
	}
	return entryID, nil
}

// markProcessedTx records a source event for idempotency inside tx. It returns
// false when the event was already recorded (a concurrent booking won the race).
func markProcessedTx(ctx context.Context, tx pgx.Tx, eventID, eventType string) (bool, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO processed_events (event_id, event_type) VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING
	`, eventID, eventType)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ErrEntryNotFound indicates the referenced journal entry does not exist.
var ErrEntryNotFound = errors.New("journal entry not found")

// ErrNotReversible indicates the entry is not in a state that can be reversed
// (only a 'posted' entry can be reversed; a draft or already-reversed cannot).
var ErrNotReversible = errors.New("only a posted entry can be reversed")

// ReverseEntry posts a mirror-image entry that cancels a posted entry and marks
// the original 'reversed' — all in one transaction. This is the correct way to
// undo a posted entry (which is otherwise immutable). It is naturally
// idempotent: a second attempt finds the original already 'reversed' and
// returns ErrNotReversible.
func (r *Repository) ReverseEntry(ctx context.Context, originalID uuid.UUID, reason string, audit *AuditInfo) (*domain.JournalEntry, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var status, entryNumber string
	err = tx.QueryRow(ctx, `
		SELECT status, entry_number FROM journal_entries WHERE id = $1 FOR UPDATE
	`, originalID).Scan(&status, &entryNumber)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrEntryNotFound
		}
		return nil, err
	}
	if status != "posted" {
		return nil, ErrNotReversible
	}

	rows, err := tx.Query(ctx, `
		SELECT account_id, debit, credit, memo, line_order
		FROM journal_lines WHERE journal_entry_id = $1 ORDER BY line_order
	`, originalID)
	if err != nil {
		return nil, err
	}
	var reversed []ResolvedLine
	for rows.Next() {
		var l ResolvedLine
		if err := rows.Scan(&l.AccountID, &l.Debit, &l.Credit, &l.Memo, &l.LineOrder); err != nil {
			rows.Close()
			return nil, err
		}
		// Mirror: a debit becomes a credit and vice versa.
		l.Debit, l.Credit = l.Credit, l.Debit
		l.Memo = "Reversal: " + l.Memo
		reversed = append(reversed, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	revNumber, err := nextEntryNumberTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	desc := "Reversal of " + entryNumber
	if reason != "" {
		desc += " — " + reason
	}
	var revID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO journal_entries (entry_number, description, status, reverses_entry_id, posted_at)
		VALUES ($1, $2, 'posted', $3, NOW())
		RETURNING id
	`, revNumber, desc, originalID).Scan(&revID); err != nil {
		return nil, err
	}
	for _, l := range reversed {
		if _, err := tx.Exec(ctx, `
			INSERT INTO journal_lines (journal_entry_id, account_id, debit, credit, memo, line_order)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, revID, l.AccountID, l.Debit, l.Credit, l.Memo, l.LineOrder); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE journal_entries SET status = 'reversed', updated_at = NOW() WHERE id = $1
	`, originalID); err != nil {
		return nil, err
	}
	if err := appendAudit(ctx, tx, audit); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetJournalEntry(ctx, revID)
}

// BookSideEffect runs inside the booking transaction once the posted entry
// exists. It receives the live tx and the new entry id so its writes commit
// atomically with the journal entry (or roll back together on error).
type BookSideEffect func(ctx context.Context, tx pgx.Tx, entryID uuid.UUID) error

// BookPostedEntry atomically books a posted journal entry, records the source
// event for idempotency, runs an optional side-effect, and commits — all in one
// transaction. When eventID is non-empty and already processed (including a
// concurrent racer that wins on the processed_events PK or the source_event_id
// unique index), it returns the previously booked entry instead of duplicating.
//
// This is the single booking primitive shared by the event consumer, manual
// posting, payments, adjustments, reconciliation settlement, and reversals.
func (r *Repository) BookPostedEntry(ctx context.Context, p CreateJournalParams, eventID, eventType string, postedAt time.Time, side BookSideEffect, audit *AuditInfo) (*domain.JournalEntry, error) {
	if eventID != "" {
		processed, err := r.IsEventProcessed(ctx, eventID)
		if err != nil {
			return nil, err
		}
		if processed {
			return r.GetJournalEntryBySourceEvent(ctx, eventID)
		}
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	entryNumber, err := nextEntryNumberTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	p.EntryNumber = entryNumber

	entryID, err := r.insertPostedEntryTx(ctx, tx, p, postedAt)
	if err != nil {
		// A concurrent booking of the same source event committed first and
		// won the partial-unique index on source_event_id — return its entry.
		if eventID != "" && isUniqueViolation(err) {
			_ = tx.Rollback(ctx)
			return r.GetJournalEntryBySourceEvent(ctx, eventID)
		}
		return nil, err
	}

	if eventID != "" {
		won, err := markProcessedTx(ctx, tx, eventID, eventType)
		if err != nil {
			return nil, err
		}
		if !won {
			_ = tx.Rollback(ctx)
			return r.GetJournalEntryBySourceEvent(ctx, eventID)
		}
	}

	if side != nil {
		if err := side(ctx, tx, entryID); err != nil {
			return nil, err
		}
	}

	if err := appendAudit(ctx, tx, audit); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetJournalEntry(ctx, entryID)
}
