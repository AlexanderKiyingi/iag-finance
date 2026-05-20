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
	ID       int64     `json:"id"`
	Ts       time.Time `json:"ts"`
	Actor    string    `json:"actor"`
	EventType string   `json:"type"`
	Message  string    `json:"message"`
	PrevHash string    `json:"prevHash"`
	Hash     string    `json:"hash"`
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

func cacheKey(tenant string) string {
	return "iag:audit:" + tenant
}

func (s *Store) Invalidate(ctx context.Context, tenant string) {
	_ = s.Redis.Del(ctx, cacheKey(tenant)).Err()
}

func (s *Store) Append(ctx context.Context, tenant string, in AppendInput) (*Event, error) {
	tx, err := s.Pg.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var prevHash string
	err = tx.QueryRow(ctx, `
		SELECT event_hash FROM audit_events WHERE tenant_id = $1 ORDER BY id DESC LIMIT 1
	`, tenant).Scan(&prevHash)
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
		INSERT INTO audit_events (tenant_id, occurred_at, actor, event_type, message, prev_hash, event_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`, tenant, ts, in.Actor, in.Type, in.Message, prevHash, h).Scan(&id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	s.Invalidate(ctx, tenant)
	return &Event{
		ID: id, Ts: ts, Actor: in.Actor, EventType: in.Type, Message: in.Message,
		PrevHash: prevHash, Hash: h,
	}, nil
}

func (s *Store) List(ctx context.Context, tenant string, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 250
	}
	key := cacheKey(tenant)
	if s.Redis != nil {
		if raw, err := s.Redis.Get(ctx, key).Bytes(); err == nil && len(raw) > 0 {
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
			WHERE tenant_id = $1
			ORDER BY id DESC
			LIMIT $2
		) AS tail
		ORDER BY id ASC
	`, tenant, limit)
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
			_ = s.Redis.Set(ctx, key, b, 20*time.Second).Err()
		}
	}
	return list, nil
}
