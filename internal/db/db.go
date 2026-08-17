package db

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	platformdb "github.com/alvor-technologies/iag-platform-go/db"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

//go:embed seed/demo.sql
var demoSeedSQL string

//go:embed seed/operational.sql
var operationalSeedSQL string

const (
	financeSchema          = "finance"
	financeMigrationsTable = "finance_schema_migrations"
)

// Connect opens the finance pool through the shared platform pool package.
//
// It previously parsed the DSN here and took pgx's defaults for everything
// else: no connection ceiling, no warm minimum, and no lifetime bound. On a
// platform where ~23 services share one Postgres instance, an unbounded pool
// per service is how "slow" becomes "too many clients", and no minimum means a
// burst of traffic pays TCP and TLS setup before its first query.
//
// The search_path also moves. It was applied with an AfterConnect `SET`, which
// is correct against Postgres directly and silently wrong behind PgBouncer in
// transaction pooling mode: the SET binds to one server connection while the
// next transaction may land on another, and finance's tables would be looked
// up in the wrong schema. The shared package sets it as a startup parameter,
// which the pooler tracks per server connection.
//
// Pool sizing is env-tunable (DB_MAX_CONNS, DB_MIN_CONNS, DB_POOLER_MODE, ...)
// so it can be adjusted per environment without a deploy.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg := platformdb.ConfigFromEnv(financeSchema + ", public")
	// The caller's URL wins: tests and tools pass an explicit DSN, and
	// ConfigFromEnv would otherwise silently substitute $DATABASE_URL.
	if strings.TrimSpace(databaseURL) != "" {
		cfg.URL = databaseURL
	}
	pool, err := platformdb.Connect(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	return pool, nil
}

func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	// Isolated from other services sharing iag_platform. Several backends ship
	// 001_init.sql; a global schema_migrations table causes false "already applied"
	// skips and legacy NOT NULL checksum columns break INSERT (version-only).
	if _, err := pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS finance`); err != nil {
		return fmt.Errorf("create finance schema: %w", err)
	}
	if _, err := pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto`); err != nil {
		return fmt.Errorf("ensure pgcrypto extension: %w", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s.%s (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			checksum TEXT NOT NULL DEFAULT ''
		)
	`, financeSchema, financeMigrationsTable)); err != nil {
		return fmt.Errorf("create %s: %w", financeMigrationsTable, err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		ALTER TABLE %s.%s ADD COLUMN IF NOT EXISTS checksum TEXT NOT NULL DEFAULT ''
	`, financeSchema, financeMigrationsTable)); err != nil {
		return fmt.Errorf("ensure %s.checksum: %w", financeMigrationsTable, err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, name := range files {
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		checksum := hex.EncodeToString(sum[:])

		var storedChecksum *string
		q := fmt.Sprintf(`SELECT checksum FROM %s.%s WHERE version = $1`, financeSchema, financeMigrationsTable)
		switch err := pool.QueryRow(ctx, q, name).Scan(&storedChecksum); {
		case err == nil:
			// Already applied — verify the file content has not drifted since.
			// A mismatch means a shipped migration was edited after release,
			// which a version-only skip would hide. Warn loudly; do not re-run.
			if storedChecksum != nil && *storedChecksum != "" && *storedChecksum != checksum {
				slog.Warn("applied migration content has drifted from its recorded checksum",
					"migration", name, "recorded", *storedChecksum, "current", checksum)
			}
			continue
		case errors.Is(err, pgx.ErrNoRows):
			// Not applied yet — fall through to apply below.
		default:
			return err
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SET search_path TO finance, public`); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s set search_path: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(body), pgx.QueryExecModeSimpleProtocol); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", name, err)
		}
		insert := fmt.Sprintf(`INSERT INTO %s.%s (version, checksum) VALUES ($1, $2)`, financeSchema, financeMigrationsTable)
		if _, err := tx.Exec(ctx, insert, name, checksum); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

func RunDemoSeed(ctx context.Context, pool *pgxpool.Pool) error {
	if demoSeedSQL == "" {
		return nil
	}
	_, err := pool.Exec(ctx, demoSeedSQL)
	if err != nil {
		return fmt.Errorf("demo seed: %w", err)
	}
	return nil
}

func RunOperationalSeed(ctx context.Context, pool *pgxpool.Pool) error {
	if operationalSeedSQL == "" {
		return nil
	}
	_, err := pool.Exec(ctx, operationalSeedSQL)
	if err != nil {
		return fmt.Errorf("operational seed: %w", err)
	}
	return nil
}

type PoolHealth struct {
	Pool *pgxpool.Pool
}

func (p *PoolHealth) Ping(ctx context.Context) error {
	return p.Pool.Ping(ctx)
}
