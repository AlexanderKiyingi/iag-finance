package config

import (
	"os"
	"testing"
)

func TestLoadProductionRequiresGateway(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgresql://svc_finance:pass@db.example.com:5432/iag_platform?sslmode=require")
	t.Setenv("AUTH_MODE", "gateway")
	t.Setenv("GATEWAY_INTERNAL_SECRET", "prod-gateway-secret-min-16")
	t.Setenv("SEED_ON_STARTUP", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected valid production config: %v", err)
	}
	if cfg.Port != 3006 {
		t.Fatalf("port: %d", cfg.Port)
	}
	if cfg.AuthMode != "gateway" {
		t.Fatalf("auth mode: %s", cfg.AuthMode)
	}
}

func TestLoadRejectsProductionSeed(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgresql://svc_finance:pass@db.example.com:5432/iag_platform?sslmode=require")
	t.Setenv("AUTH_MODE", "gateway")
	t.Setenv("GATEWAY_INTERNAL_SECRET", "prod-gateway-secret-min-16")
	t.Setenv("SEED_ON_STARTUP", "true")

	if _, err := Load(); err == nil {
		t.Fatal("expected error for SEED_ON_STARTUP in production")
	}
}

func TestLoadDevelopmentDefaults(t *testing.T) {
	os.Unsetenv("ENVIRONMENT")
	os.Unsetenv("AUTH_MODE")
	t.Setenv("DATABASE_URL", "postgresql://svc_finance:iag_finance_dev@localhost:5432/iag_platform?sslmode=disable")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.AutoMigrate != true {
		t.Fatal("expected AUTO_MIGRATE true by default")
	}
}
