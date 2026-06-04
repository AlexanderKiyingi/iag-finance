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
	"github.com/redis/go-redis/v9"

	platformevents "github.com/alvor-technologies/iag-platform-go/events"
	platformotel "github.com/alvor-technologies/iag-platform-go/otel"

	platformserviceauth "github.com/alvor-technologies/iag-platform-go/serviceauth"

	"github.com/iag-finance/backend/internal/api"
	"github.com/iag-finance/backend/internal/authclient"
	"github.com/iag-finance/backend/internal/config"
	"github.com/iag-finance/backend/internal/consumer"
	"github.com/iag-finance/backend/internal/db"
	"github.com/iag-finance/backend/internal/events"
	"github.com/iag-finance/backend/internal/models"
	"github.com/iag-finance/backend/internal/repository"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	gin.SetMode(cfg.GinMode())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tp, err := platformotel.Init(ctx, platformotel.Config{
		ServiceName: cfg.ServiceName,
		Environment: cfg.Environment,
	})
	if err != nil {
		slog.Warn("otel disabled", "error", err)
	} else {
		defer func() {
			sc, c := context.WithTimeout(context.Background(), 5*time.Second)
			defer c()
			_ = tp.Shutdown(sc)
		}()
	}

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
	repo := repository.New(pool)

	if cfg.ServiceClientSecret != "" {
		go registerPermissionsLoop(ctx, cfg)
	} else {
		slog.Warn("SERVICE_CLIENT_SECRET unset — skipping permissions registration")
	}

	var eventBus *events.Bus
	if cfg.EnableEventPublish && len(cfg.KafkaBrokers) > 0 {
		eventBus = events.New(events.Config{
			Brokers:  cfg.KafkaBrokers,
			ClientID: cfg.KafkaClientID,
			Topic:    cfg.KafkaTopic,
			Enabled:  true,
		})
		defer func() { _ = eventBus.Close() }()
		slog.Info("finance event publisher enabled", "topic", cfg.KafkaTopic)
	}

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

	// Inbound verifier — every request must carry aud=cfg.Audience.
	verifier := authclient.NewVerifier(cfg.JWKSURL, cfg.JWTIssuer, cfg.Audience)
	if err := verifier.Refresh(ctx); err != nil {
		slog.Warn("jwks refresh failed", "error", err)
	}
	verifier.StartRefreshLoop(ctx, 15*time.Minute)

	router := api.NewRouter(api.RouterDeps{
		Config:   cfg,
		Pool:     pool,
		Redis:    rdb,
		Verifier: verifier,
		Ledger:   ledgerSvc,
		AuditLog: auditSvc,
		Events:   eventBus,
	})

	// Shared producer used for the DLQ. Re-used by both the iag.finance and
	// iag.fleet consumers below so we don't carry two Kafka writer fleets.
	var dlqProducer *platformevents.Producer
	if cfg.EnableConsumer && len(cfg.KafkaBrokers) > 0 {
		dlqProducer = platformevents.NewProducer(platformevents.ProducerConfig{
			Brokers:  cfg.KafkaBrokers,
			ClientID: cfg.KafkaClientID,
		})
		defer func() { _ = dlqProducer.Close() }()
	}

	var consumers []*consumer.Consumer
	if cfg.EnableConsumer {
		// Primary: iag.finance — accounting events (sale.completed, invoice.posted).
		c1, err := consumer.New(consumer.Config{
			Brokers:  cfg.KafkaBrokers,
			GroupID:  cfg.KafkaGroupID,
			Topic:    cfg.KafkaTopic,
			DLQTopic: cfg.KafkaDLQTopic,
		}, ledgerSvc, auditSvc, dlqProducer)
		if err != nil {
			log.Fatal("finance consumer: ", err)
		}
		consumers = append(consumers, c1)

		// Secondary: iag.fleet — fleet.fuel.recorded (and any future fleet
		// events that finance cares about). Distinct consumer group so the two
		// streams advance independently.
		c2, err := consumer.New(consumer.Config{
			Brokers:  cfg.KafkaBrokers,
			GroupID:  "iag.finance.fleet",
			Topic:    "iag.fleet",
			DLQTopic: cfg.KafkaDLQTopic,
		}, ledgerSvc, auditSvc, dlqProducer)
		if err != nil {
			log.Fatal("finance fleet consumer: ", err)
		}
		consumers = append(consumers, c2)

		// Tertiary: iag.supply-chain — scm.party.* for AP party_id backfill.
		c3, err := consumer.NewSupplyChain(consumer.Config{
			Brokers:  cfg.KafkaBrokers,
			GroupID:  "iag.finance.supply-chain",
			Topic:    cfg.KafkaSupplyChainTopic,
			DLQTopic: cfg.KafkaDLQTopic,
		}, repo, dlqProducer)
		if err != nil {
			log.Fatal("finance supply-chain consumer: ", err)
		}
		consumers = append(consumers, c3)

		for _, c := range consumers {
			c := c
			go func() {
				if err := c.Run(ctx); err != nil {
					slog.Error("finance consumer stopped", "error", err)
				}
			}()
		}
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
			"audience", cfg.Audience,
			"consumer", cfg.EnableConsumer,
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	shutdownCtx, c := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer c()
	for _, cn := range consumers {
		_ = cn.Close()
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Warn("shutdown", "error", err)
	}
	if rdb != nil {
		_ = rdb.Close()
	}
}

func registerPermissionsLoop(ctx context.Context, cfg config.Config) {
	saClient := platformserviceauth.NewClient(platformserviceauth.Options{
		TokenURL:     cfg.AuthTokenURL,
		ClientID:     cfg.ServiceClientID,
		ClientSecret: cfg.ServiceClientSecret,
		Audience:     "iag.authentication",
	})
	descriptors := models.PermissionDescriptors()
	perms := make([]platformserviceauth.Permission, 0, len(descriptors))
	for _, d := range descriptors {
		perms = append(perms, platformserviceauth.Permission{
			Name:        d.Name,
			Description: d.Description,
		})
	}

	backoff := time.Second
	const maxBackoff = 5 * time.Minute
	for {
		regCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := platformserviceauth.RegisterPermissions(regCtx, saClient, cfg.JWTIssuer, "finance", perms)
		cancel()
		if err == nil {
			slog.Info("permissions registered with auth service", "count", len(perms))
			return
		}
		slog.Warn("permissions registration failed; retrying", "err", err, "backoff", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
		}
	}
}
