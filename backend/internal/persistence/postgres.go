package persistence

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/iag/finance-backend/internal/models"
	"github.com/iag/finance-backend/internal/security"
)

type Postgres struct {
	Pool *pgxpool.Pool
}

func Connect(ctx context.Context, databaseURL string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	pg := &Postgres{Pool: pool}
	if err := RunMigrations(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return pg, nil
}

func (p *Postgres) Close() { p.Pool.Close() }

func (p *Postgres) LoadState(ctx context.Context) (*models.PersistedState, error) {
	var raw []byte
	err := p.Pool.QueryRow(ctx, `SELECT state FROM finance_app_state WHERE id=1`).Scan(&raw)
	if err == pgx.ErrNoRows {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var st models.PersistedState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (p *Postgres) SaveState(ctx context.Context, state *models.PersistedState) error {
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = p.Pool.Exec(ctx, `
		INSERT INTO finance_app_state (id, state, updated_at) VALUES (1, $1, NOW())
		ON CONFLICT (id) DO UPDATE SET state = EXCLUDED.state, updated_at = NOW()`, b)
	return err
}

func (p *Postgres) IsEmpty(ctx context.Context) (bool, error) {
	var n int
	err := p.Pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM finance_app_state WHERE id=1`).Scan(&n)
	return n == 0, err
}

func (p *Postgres) FindAuthAccount(ctx context.Context, email string) (models.AuthAccount, error) {
	var a models.AuthAccount
	var hash string
	err := p.Pool.QueryRow(ctx, `
		SELECT email, password_hash, role, display_name, entity FROM auth_accounts WHERE lower(email)=lower($1)`, email).
		Scan(&a.Email, &hash, &a.Role, &a.DisplayName, &a.Entity)
	if err == pgx.ErrNoRows {
		return models.AuthAccount{}, models.ErrNotFound
	}
	if err != nil {
		return models.AuthAccount{}, err
	}
	a.Password = hash
	return a, nil
}

func (p *Postgres) ListAuthAccounts(ctx context.Context) ([]models.AuthAccount, error) {
	rows, err := p.Pool.Query(ctx, `SELECT email, role, display_name, entity FROM auth_accounts ORDER BY email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.AuthAccount
	for rows.Next() {
		var a models.AuthAccount
		if err := rows.Scan(&a.Email, &a.Role, &a.DisplayName, &a.Entity); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (p *Postgres) SeedAuthAccounts(ctx context.Context, accounts []models.AuthAccount) error {
	for _, a := range accounts {
		hash, err := security.HashPassword(a.Password)
		if err != nil {
			return err
		}
		_, err = p.Pool.Exec(ctx, `
			INSERT INTO auth_accounts (email, password_hash, role, display_name, entity)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (email) DO NOTHING`, a.Email, hash, a.Role, a.DisplayName, a.Entity)
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *Postgres) Ping(ctx context.Context) error {
	return p.Pool.Ping(ctx)
}

func (p *Postgres) SeedStateIfEmpty(ctx context.Context, state *models.PersistedState) error {
	empty, err := p.IsEmpty(ctx)
	if err != nil {
		return err
	}
	if !empty {
		return nil
	}
	if err := p.SaveState(ctx, state); err != nil {
		return fmt.Errorf("seed state: %w", err)
	}
	return p.SeedAuthAccounts(ctx, models.BuiltinAuthAccounts())
}
