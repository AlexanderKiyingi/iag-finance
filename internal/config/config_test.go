package config

import (
	"os"
	"testing"
)

// Post-cutover: AUTH_MODE and GATEWAY_INTERNAL_SECRET no longer exist.
// Production requires SERVICE_CLIENT_SECRET for outbound calls and a
// non-empty audience for inbound verification.
func TestLoadProductionRequiresServiceSecret(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgresql://svc_finance:pass@db.example.com:5432/iag_platform?sslmode=require")
	t.Setenv("SEED_ON_STARTUP", "false")
	t.Setenv("SERVICE_CLIENT_SECRET", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected SERVICE_CLIENT_SECRET to be required in production")
	}
}

func TestLoadProductionValid(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgresql://svc_finance:pass@db.example.com:5432/iag_platform?sslmode=require")
	t.Setenv("SEED_ON_STARTUP", "false")
	t.Setenv("SERVICE_CLIENT_SECRET", "a-secret-of-meaningful-length")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected valid production config: %v", err)
	}
	if cfg.Port != 3006 {
		t.Fatalf("port: %d", cfg.Port)
	}
	if cfg.Audience != "iag.finance" {
		t.Fatalf("audience: %s", cfg.Audience)
	}
}

func TestLoadRejectsProductionSeed(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgresql://svc_finance:pass@db.example.com:5432/iag_platform?sslmode=require")
	t.Setenv("SEED_ON_STARTUP", "true")
	t.Setenv("SERVICE_CLIENT_SECRET", "a-secret-of-meaningful-length")

	if _, err := Load(); err == nil {
		t.Fatal("expected error for SEED_ON_STARTUP in production")
	}
}

func TestLoadDevelopmentDefaults(t *testing.T) {
	os.Unsetenv("ENVIRONMENT")
	t.Setenv("DATABASE_URL", "postgresql://svc_finance:iag_finance_dev@localhost:5432/iag_platform?sslmode=disable")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.AutoMigrate != true {
		t.Fatal("expected AUTO_MIGRATE true by default")
	}
}
