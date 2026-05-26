package app

import (
	"context"
	"log"

	"github.com/iag/finance-backend/internal/auth"
	"github.com/iag/finance-backend/internal/config"
	"github.com/iag/finance-backend/internal/models"
	"github.com/iag/finance-backend/internal/persistence"
)

func buildStore(cfg config.Config) (*models.Store, *auth.Service, func()) {
	opts := &models.StoreOptions{}
	cleanup := func() {}
	var jwtSvc *auth.Service
	var pgDep, redisDep interface{}

	if cfg.PostgresEnabled() {
		ctx := context.Background()
		pg, err := persistence.Connect(ctx, cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres: %v", err)
		}
		opts.Repo = pg
		pgDep = pg
		prev := cleanup
		cleanup = func() {
			pg.Close()
			prev()
		}
		log.Println("postgres connected")
		_ = pg.SeedStateIfEmpty(ctx, models.NewPersistedState())
	} else if cfg.DataPath != "" {
		opts.Repo = persistence.NewFileSnapshot(cfg.DataPath)
		log.Printf("file persistence: %s", cfg.DataPath)
	}

	if cfg.RedisEnabled() {
		tokens, err := persistence.ConnectRedis(cfg.RedisURL, cfg.JWTRefreshTTL)
		if err != nil {
			log.Fatalf("redis: %v", err)
		}
		opts.Tokens = tokens
		redisDep = tokens
		prev := cleanup
		cleanup = func() {
			_ = tokens.Close()
			prev()
		}
		log.Println("redis connected")
	}

	if cfg.JWTEnabled() {
		jwtSvc = auth.NewService(cfg.JWTSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
		opts.JWT = jwtSvc
		log.Println("jwt enabled")
	}

	models.SetHealthDeps(pgDep, redisDep)
	return models.NewStore(cfg.DataPath, opts), jwtSvc, cleanup
}
