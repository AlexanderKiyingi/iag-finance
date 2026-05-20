package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iag-finance/backend/internal/api"
	"github.com/iag-finance/backend/internal/authclient"
	"github.com/iag-finance/backend/internal/config"
	"github.com/iag-finance/backend/internal/consumer"
	"github.com/iag-finance/backend/internal/db"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	gin.SetMode(cfg.GinMode())

	ctx := context.Background()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	if cfg.AutoMigrate {
		if err := db.RunMigrations(ctx, pool); err != nil {
			log.Fatal("migrations: ", err)
		}
	}

	ledgerSvc, auditSvc := api.NewLedger(pool)

	if cfg.SeedOnStartup {
		if err := ledgerSvc.Seed(ctx); err != nil {
			log.Fatal("chart seed: ", err)
		}
		if err := db.RunOperationalSeed(ctx, pool); err != nil {
			log.Fatal("operational seed: ", err)
		}
		if err := db.RunDemoSeed(ctx, pool); err != nil {
			log.Fatal("demo seed: ", err)
		}
		slog.Info("finance seeds applied")
	}

	var rdb *redis.Client
	if cfg.RedisURL != "" {
		opts, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			log.Fatalf("redis url: %v", err)
		}
		rdb = redis.NewClient(opts)
		if err := rdb.Ping(ctx).Err(); err != nil {
			slog.Warn("redis unavailable; chain audit cache disabled", "error", err)
			_ = rdb.Close()
			rdb = nil
		}
	}

	var verifier *authclient.Verifier
	if cfg.AuthMode == "jwt" {
		verifier = authclient.NewVerifier(cfg.JWKSURL, cfg.JWTIssuer)
		if err := verifier.Refresh(ctx); err != nil {
			slog.Warn("jwks refresh failed", "error", err)
		} else {
			go jwksRefreshLoop(verifier)
		}
	}

	router := api.NewRouter(api.RouterDeps{
		Config:   cfg,
		Pool:     pool,
		Redis:    rdb,
		Verifier: verifier,
		Ledger:   ledgerSvc,
		AuditLog: auditSvc,
	})

	var financeConsumer *consumer.Consumer
	if cfg.EnableConsumer {
		financeConsumer = consumer.New(consumer.Config{
			Brokers: cfg.KafkaBrokers,
			GroupID: cfg.KafkaGroupID,
			Topic:   cfg.KafkaTopic,
		}, ledgerSvc, auditSvc)
		go func() {
			if err := financeConsumer.Run(ctx); err != nil {
				slog.Error("finance consumer stopped", "error", err)
			}
		}()
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           router,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}

	go func() {
		slog.Info("finance service listening",
			"environment", cfg.Environment,
			"port", cfg.Port,
			"auth_mode", cfg.AuthMode,
			"consumer", cfg.EnableConsumer,
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if financeConsumer != nil {
		_ = financeConsumer.Close()
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Warn("shutdown", "error", err)
	}
	if rdb != nil {
		_ = rdb.Close()
	}
}

func jwksRefreshLoop(v *authclient.Verifier) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		if err := v.Refresh(context.Background()); err != nil {
			slog.Warn("jwks refresh", "error", err)
		}
	}
}
