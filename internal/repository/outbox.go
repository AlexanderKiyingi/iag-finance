package repository

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// OutboxEvent is a domain event to be delivered after the transaction that
// produced it commits. Written to event_outbox in the same tx as the state
// change, then published at-least-once by the relay worker.
type OutboxEvent struct {
	Topic        string
	PartitionKey string
	EventID      string
	EventType    string
	Payload      map[string]any
}

// enqueueOutboxTx writes an outbox row inside tx. Idempotent on event_id, so a
// retried producer does not enqueue duplicates.
func enqueueOutboxTx(ctx context.Context, tx pgx.Tx, ev OutboxEvent) error {
	payload, err := json.Marshal(ev.Payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO event_outbox (topic, partition_key, event_id, event_type, payload)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (event_id) DO NOTHING
	`, ev.Topic, ev.PartitionKey, ev.EventID, ev.EventType, payload)
	return err
}

// EnqueueOutbox writes an outbox row outside any caller transaction. Use the
// in-tx path (passing *OutboxEvent to a create method) when the event must be
// atomic with a state change; use this for events that stand on their own.
func (r *Repository) EnqueueOutbox(ctx context.Context, ev OutboxEvent) error {
	payload, err := json.Marshal(ev.Payload)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO event_outbox (topic, partition_key, event_id, event_type, payload)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (event_id) DO NOTHING
	`, ev.Topic, ev.PartitionKey, ev.EventID, ev.EventType, payload)
	return err
}

// OutboxRow is an unpublished event the relay worker must deliver.
type OutboxRow struct {
	ID           uuid.UUID
	Topic        string
	PartitionKey string
	EventID      string
	EventType    string
	Payload      []byte
	Attempts     int
}

// FetchUnpublishedOutbox returns the oldest unpublished events, capped at limit.
func (r *Repository) FetchUnpublishedOutbox(ctx context.Context, limit int) ([]OutboxRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, topic, partition_key, event_id, event_type, payload::text, attempts
		FROM event_outbox
		WHERE published_at IS NULL
		ORDER BY created_at
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxRow
	for rows.Next() {
		var o OutboxRow
		var payload string
		if err := rows.Scan(&o.ID, &o.Topic, &o.PartitionKey, &o.EventID, &o.EventType, &payload, &o.Attempts); err != nil {
			return nil, err
		}
		o.Payload = []byte(payload)
		out = append(out, o)
	}
	return out, rows.Err()
}

// MarkOutboxPublished stamps an event delivered.
func (r *Repository) MarkOutboxPublished(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE event_outbox SET published_at = NOW() WHERE id = $1`, id)
	return err
}

// MarkOutboxFailed records a delivery attempt failure for backoff/visibility.
func (r *Repository) MarkOutboxFailed(ctx context.Context, id uuid.UUID, errMsg string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE event_outbox SET attempts = attempts + 1, last_error = $2 WHERE id = $1
	`, id, errMsg)
	return err
}
