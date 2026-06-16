package chainaudit

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/iag-finance/backend/internal/hashchain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// chainLockKey serializes every append to the audit hash chain (HTTP appends
// and GL-mutation appends alike) via a transaction-scoped advisory lock, so the
// prev_hash is always read under lock and concurrent writers cannot fork the
// chain. The constant is an arbitrary, stable 64-bit key.
const chainLockKey int64 = 0x1A6F1_4D17_C8A1

type Event struct {
	ID        int64     `json:"id"`
	Ts        time.Time `json:"ts"`
	Actor     string    `json:"actor"`
	EventType string    `json:"type"`
	Message   string    `json:"message"`
	PrevHash  string    `json:"prevHash"`
	Hash      string    `json:"hash"`
}

type AppendInput struct {
	Type    string `json:"type" binding:"required"`
	Message string `json:"message" binding:"required"`
	// Actor is set server-side from the authenticated principal, never from the
	// request body — a caller must not be able to forge attribution.
	Actor string `json:"-"`
}

type Store struct {
	Pg    *pgxpool.Pool
	Redis *redis.Client
}

const cacheKey = "iag:finance:audit"

func (s *Store) Invalidate(ctx context.Context) {
	if s.Redis != nil {
		_ = s.Redis.Del(ctx, cacheKey).Err()
	}
}

// AppendChainTx appends one event to the hash chain inside an existing
// transaction. It takes the chain advisory lock, reads the current head, links
// the new event to it, and inserts. Because it runs in the caller's tx, the
// audit row commits atomically with whatever mutation it describes (or rolls
// back together). Returns the inserted event.
func AppendChainTx(ctx context.Context, tx pgx.Tx, actor, eventType, message string) (*Event, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, chainLockKey); err != nil {
		return nil, err
	}

	prevHash := "GENESIS"
	err := tx.QueryRow(ctx, `SELECT event_hash FROM audit_events ORDER BY id DESC LIMIT 1`).Scan(&prevHash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	ts := time.Now().UTC()
	h := hashchain.EventHash(ts.Format(time.RFC3339Nano), actor, eventType, message, prevHash)

	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO audit_events (occurred_at, actor, event_type, message, prev_hash, event_hash)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, ts, actor, eventType, message, prevHash, h).Scan(&id); err != nil {
		return nil, err
	}
	return &Event{ID: id, Ts: ts, Actor: actor, EventType: eventType, Message: message, PrevHash: prevHash, Hash: h}, nil
}

// Append writes one audit event via its own transaction (used by the HTTP
// /audit/events endpoint).
func (s *Store) Append(ctx context.Context, in AppendInput) (*Event, error) {
	tx, err := s.Pg.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	ev, err := AppendChainTx(ctx, tx, in.Actor, in.Type, in.Message)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	s.Invalidate(ctx)
	return ev, nil
}

// VerifyResult reports whether the chain is intact and, if not, where it broke.
type VerifyResult struct {
	Valid    bool   `json:"valid"`
	Count    int    `json:"count"`
	BrokenAt *int64 `json:"brokenAt,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Verify walks the chain from genesis, recomputing each event's hash and
// checking that each prev_hash matches the previous event's hash. It detects
// in-place tampering of any field as well as deletions/reordering.
func (s *Store) Verify(ctx context.Context) (VerifyResult, error) {
	rows, err := s.Pg.Query(ctx, `
		SELECT id, occurred_at, actor, event_type, message, prev_hash, event_hash
		FROM audit_events ORDER BY id ASC
	`)
	if err != nil {
		return VerifyResult{}, err
	}
	defer rows.Close()

	prevHash := "GENESIS"
	count := 0
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Ts, &e.Actor, &e.EventType, &e.Message, &e.PrevHash, &e.Hash); err != nil {
			return VerifyResult{}, err
		}
		count++
		if e.PrevHash != prevHash {
			id := e.ID
			return VerifyResult{Valid: false, Count: count, BrokenAt: &id, Reason: "prev_hash does not match the previous event"}, nil
		}
		want := hashchain.EventHash(e.Ts.UTC().Format(time.RFC3339Nano), e.Actor, e.EventType, e.Message, e.PrevHash)
		if want != e.Hash {
			id := e.ID
			return VerifyResult{Valid: false, Count: count, BrokenAt: &id, Reason: "event_hash does not match recomputed hash"}, nil
		}
		prevHash = e.Hash
	}
	if err := rows.Err(); err != nil {
		return VerifyResult{}, err
	}
	return VerifyResult{Valid: true, Count: count}, nil
}

func (s *Store) List(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 250
	}
	if s.Redis != nil {
		if raw, err := s.Redis.Get(ctx, cacheKey).Bytes(); err == nil && len(raw) > 0 {
			var cached []Event
			if json.Unmarshal(raw, &cached) == nil && len(cached) > 0 {
				return cached, nil
			}
		}
	}

	rows, err := s.Pg.Query(ctx, `
		SELECT id, occurred_at, actor, event_type, message, prev_hash, event_hash
		FROM (
			SELECT id, occurred_at, actor, event_type, message, prev_hash, event_hash
			FROM audit_events
			ORDER BY id DESC
			LIMIT $1
		) AS tail
		ORDER BY id ASC
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Ts, &e.Actor, &e.EventType, &e.Message, &e.PrevHash, &e.Hash); err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if s.Redis != nil && len(list) > 0 {
		if b, err := json.Marshal(list); err == nil {
			_ = s.Redis.Set(ctx, cacheKey, b, 20*time.Second).Err()
		}
	}
	return list, nil
}
