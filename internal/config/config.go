package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ServiceName       string
	Environment       string
	Port              int
	DatabaseURL       string
	RedisURL          string
	JWTIssuer         string
	JWKSURL           string
	LogLevel          string
	AuthMode          string
	GatewaySecret     string
	SeedOnStartup     bool
	AutoMigrate       bool
	CORSAllowOrigins  []string
	EnableConsumer    bool
	KafkaBrokers      []string
	KafkaClientID     string
	KafkaGroupID      string
	KafkaTopic        string
	ShutdownTimeout   time.Duration
	ReadHeaderTimeout time.Duration
}

func Load() (Config, error) {
	loadDotEnv()

	env := strings.ToLower(getEnv("ENVIRONMENT", "development"))
	if env != "development" && env != "staging" && env != "production" {
		return Config{}, fmt.Errorf("ENVIRONMENT must be development, staging, or production")
	}

	port, err := strconv.Atoi(getEnv("PORT", "3006"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid PORT: %w", err)
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	issuer := getEnv("JWT_ISSUER", "http://localhost:3001")
	jwksURL := getEnv("JWKS_URL", strings.TrimRight(issuer, "/")+"/.well-known/jwks.json")

	authMode := getEnv("AUTH_MODE", defaultAuthMode(env))
	if authMode != "gateway" && authMode != "jwt" {
		return Config{}, fmt.Errorf("AUTH_MODE must be gateway or jwt")
	}

	seedDefault := "true"
	if env == "production" {
		seedDefault = "false"
	}

	shutdownSec, err := strconv.Atoi(getEnv("SHUTDOWN_TIMEOUT_SECONDS", "15"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid SHUTDOWN_TIMEOUT_SECONDS: %w", err)
	}

	corsRaw := getEnv("CORS_ALLOW_ORIGIN", "http://localhost:3000,http://localhost:5173")
	var corsOrigins []string
	for _, p := range strings.Split(corsRaw, ",") {
		if s := strings.TrimSpace(p); s != "" {
			corsOrigins = append(corsOrigins, s)
		}
	}

	cfg := Config{
		ServiceName:       getEnv("SERVICE_NAME", "finance"),
		Environment:       env,
		Port:              port,
		DatabaseURL:       dbURL,
		RedisURL:          strings.TrimSpace(os.Getenv("REDIS_URL")),
		JWTIssuer:         issuer,
		JWKSURL:           jwksURL,
		LogLevel:          getEnv("LOG_LEVEL", "info"),
		AuthMode:          authMode,
		GatewaySecret:     os.Getenv("GATEWAY_INTERNAL_SECRET"),
		SeedOnStartup:     getEnv("SEED_ON_STARTUP", seedDefault) == "true",
		AutoMigrate:       getEnv("AUTO_MIGRATE", "true") != "false",
		CORSAllowOrigins:  corsOrigins,
		EnableConsumer:    getEnv("ENABLE_CONSUMER", defaultConsumer(env)) == "true",
		KafkaBrokers:      splitBrokers(getEnv("KAFKA_BROKERS", "localhost:19092")),
		KafkaClientID:     getEnv("KAFKA_CLIENT_ID", "finance"),
		KafkaGroupID:      getEnv("KAFKA_GROUP_ID", "iag.finance.ledger"),
		KafkaTopic:        getEnv("KAFKA_FINANCE_TOPIC", "iag.finance"),
		ShutdownTimeout:   time.Duration(shutdownSec) * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return cfg, cfg.validate()
}

func (c Config) validate() error {
	if c.Environment == "production" {
		if c.AuthMode != "gateway" {
			return fmt.Errorf("production requires AUTH_MODE=gateway")
		}
		if c.GatewaySecret == "" {
			return fmt.Errorf("production requires GATEWAY_INTERNAL_SECRET")
		}
		if len(c.GatewaySecret) < 16 {
			return fmt.Errorf("GATEWAY_INTERNAL_SECRET must be at least 16 characters in production")
		}
		if c.SeedOnStartup {
			return fmt.Errorf("production must not enable SEED_ON_STARTUP")
		}
	}
	if c.AuthMode == "gateway" && c.GatewaySecret == "" {
		return fmt.Errorf("AUTH_MODE=gateway requires GATEWAY_INTERNAL_SECRET")
	}
	return nil
}

func (c Config) GinMode() string {
	if c.Environment == "development" {
		return "debug"
	}
	return "release"
}

func defaultConsumer(env string) string {
	if env == "production" || env == "staging" {
		return "true"
	}
	return "false"
}

func splitBrokers(raw string) []string {
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func defaultAuthMode(env string) string {
	if env == "production" || env == "staging" {
		return "gateway"
	}
	return "jwt"
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func loadDotEnv() {
	if _, err := os.Stat(".env"); err != nil {
		return
	}
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		if k != "" && os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
}
