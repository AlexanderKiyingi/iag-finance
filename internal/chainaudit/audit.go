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
	Actor   string `json:"actor" binding:"required"`
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

func (s *Store) Append(ctx context.Context, in AppendInput) (*Event, error) {
	tx, err := s.Pg.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var prevHash string
	err = tx.QueryRow(ctx, `
		SELECT event_hash FROM audit_events ORDER BY id DESC LIMIT 1
	`).Scan(&prevHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			prevHash = "GENESIS"
		} else {
			return nil, err
		}
	}

	ts := time.Now().UTC()
	tsStr := ts.Format(time.RFC3339Nano)
	h := hashchain.EventHash(tsStr, in.Actor, in.Type, in.Message, prevHash)

	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO audit_events (occurred_at, actor, event_type, message, prev_hash, event_hash)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, ts, in.Actor, in.Type, in.Message, prevHash, h).Scan(&id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	s.Invalidate(ctx)
	return &Event{
		ID: id, Ts: ts, Actor: in.Actor, EventType: in.Type, Message: in.Message,
		PrevHash: prevHash, Hash: h,
	}, nil
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
