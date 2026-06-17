package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DefaultEntityID is the seeded entity that all pre-multi-entity data belongs to
// (migration 030) and the column default, so any insert without an explicit
// entity falls back to it. Single-entity deployments only ever use this one.
var DefaultEntityID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

type entityCtxKey struct{}

// WithEntity attaches the working entity id to ctx (set by the entity-context
// middleware from the request).
func WithEntity(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, entityCtxKey{}, id)
}

// EntityFromContext returns the working entity id, defaulting to the DEFAULT
// entity when none is set (consumers, workers, single-entity deployments).
func EntityFromContext(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(entityCtxKey{}).(uuid.UUID); ok && v != uuid.Nil {
		return v
	}
	return DefaultEntityID
}

type Entity struct {
	ID           uuid.UUID  `json:"id"`
	Code         string     `json:"code"`
	Name         string     `json:"name"`
	BaseCurrency string     `json:"baseCurrency"`
	ParentID     *uuid.UUID `json:"parentId,omitempty"`
	Active       bool       `json:"active"`
}

func (r *Repository) ListEntities(ctx context.Context) ([]Entity, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, code, name, base_currency, parent_id, active FROM entities ORDER BY code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entity
	for rows.Next() {
		var e Entity
		if err := rows.Scan(&e.ID, &e.Code, &e.Name, &e.BaseCurrency, &e.ParentID, &e.Active); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repository) CreateEntity(ctx context.Context, code, name, baseCurrency string, parentID *uuid.UUID) (*Entity, error) {
	if baseCurrency == "" {
		baseCurrency = r.baseCurrency
	}
	var e Entity
	err := r.pool.QueryRow(ctx, `
		INSERT INTO entities (code, name, base_currency, parent_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id, code, name, base_currency, parent_id, active
	`, code, name, baseCurrency, parentID).Scan(&e.ID, &e.Code, &e.Name, &e.BaseCurrency, &e.ParentID, &e.Active)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *Repository) GetEntityByCode(ctx context.Context, code string) (*Entity, error) {
	var e Entity
	err := r.pool.QueryRow(ctx, `
		SELECT id, code, name, base_currency, parent_id, active FROM entities WHERE code = $1
	`, code).Scan(&e.ID, &e.Code, &e.Name, &e.BaseCurrency, &e.ParentID, &e.Active)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}

// EntityScope returns the entity ids a report should read: just `root`, or `root`
// plus all its descendants (by parent_id) when consolidated.
func (r *Repository) EntityScope(ctx context.Context, root uuid.UUID, consolidated bool) ([]uuid.UUID, error) {
	if !consolidated {
		return []uuid.UUID{root}, nil
	}
	rows, err := r.pool.Query(ctx, `
		WITH RECURSIVE tree AS (
			SELECT id FROM entities WHERE id = $1
			UNION ALL
			SELECT e.id FROM entities e JOIN tree t ON e.parent_id = t.id
		)
		SELECT id FROM tree
	`, root)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		ids = []uuid.UUID{root}
	}
	return ids, rows.Err()
}
