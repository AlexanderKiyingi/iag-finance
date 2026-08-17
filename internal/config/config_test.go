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

// A localhost broker default is worse than no default. Publishing is on by
// default and the producer is synchronous and fully-acked, so in production
// with KAFKA_BROKERS unset every publish dialled a dead address from inside a
// request handler and waited for the timeout. Empty keeps the guards in
// main.go honest: no brokers, no producer, no dial.
func TestUnsetKafkaBrokersYieldsNoBrokers(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgresql://svc_finance:pass@db.example.com:5432/iag_platform?sslmode=require")
	t.Setenv("SEED_ON_STARTUP", "false")
	t.Setenv("SERVICE_CLIENT_SECRET", "a-secret-of-meaningful-length")
	t.Setenv("KAFKA_BROKERS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.KafkaBrokers) != 0 {
		t.Fatalf("expected no brokers when KAFKA_BROKERS is unset, got %#v", cfg.KafkaBrokers)
	}
}

func TestConfiguredKafkaBrokersStillParse(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgresql://svc_finance:pass@db.example.com:5432/iag_platform?sslmode=require")
	t.Setenv("SEED_ON_STARTUP", "false")
	t.Setenv("SERVICE_CLIENT_SECRET", "a-secret-of-meaningful-length")
	t.Setenv("KAFKA_BROKERS", " broker-1:9092 , broker-2:9092 ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := []string{"broker-1:9092", "broker-2:9092"}
	if len(cfg.KafkaBrokers) != len(want) {
		t.Fatalf("got %#v, want %#v", cfg.KafkaBrokers, want)
	}
	for i := range want {
		if cfg.KafkaBrokers[i] != want[i] {
			t.Fatalf("got %#v, want %#v", cfg.KafkaBrokers, want)
		}
	}
}
