package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds runtime knobs for the finance service.
type Config struct {
	ServiceName       string
	Environment       string
	Port              int
	DatabaseURL       string
	RedisURL          string
	JWTIssuer         string
	JWKSURL           string
	Audience          string // backends MUST reject tokens lacking this aud
	LogLevel          string
	SeedOnStartup     bool
	AutoMigrate       bool
	CORSAllowOrigins  []string
	EnableConsumer    bool
	KafkaBrokers      []string
	KafkaClientID     string
	KafkaGroupID      string
	KafkaTopic        string
	KafkaDLQTopic     string
	ShutdownTimeout   time.Duration
	ReadHeaderTimeout time.Duration

	// Outbound service-account credentials.
	ServiceClientID     string
	ServiceClientSecret string
	AuthTokenURL        string
}

// Load reads configuration from env. Hard cutover: no AUTH_MODE, no
// GATEWAY_INTERNAL_SECRET — every inbound request must carry a verifiable
// Bearer with aud=Audience.
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
		ServiceName:         getEnv("SERVICE_NAME", "finance"),
		Environment:         env,
		Port:                port,
		DatabaseURL:         dbURL,
		RedisURL:            strings.TrimSpace(os.Getenv("REDIS_URL")),
		JWTIssuer:           issuer,
		JWKSURL:             jwksURL,
		Audience:            getEnv("AUDIENCE", "iag.finance"),
		LogLevel:            getEnv("LOG_LEVEL", "info"),
		SeedOnStartup:       getEnv("SEED_ON_STARTUP", seedDefault) == "true",
		AutoMigrate:         getEnv("AUTO_MIGRATE", "true") != "false",
		CORSAllowOrigins:    corsOrigins,
		EnableConsumer:      getEnv("ENABLE_CONSUMER", defaultConsumer(env)) == "true",
		KafkaBrokers:        splitBrokers(getEnv("KAFKA_BROKERS", "localhost:19092")),
		KafkaClientID:       getEnv("KAFKA_CLIENT_ID", "finance"),
		KafkaGroupID:        getEnv("KAFKA_GROUP_ID", "iag.finance.ledger"),
		KafkaTopic:          getEnv("KAFKA_FINANCE_TOPIC", "iag.finance"),
		KafkaDLQTopic:       getEnv("KAFKA_DLQ_TOPIC", "iag.finance.dlq"),
		ShutdownTimeout:     time.Duration(shutdownSec) * time.Second,
		ReadHeaderTimeout:   10 * time.Second,
		ServiceClientID:     getEnv("SERVICE_CLIENT_ID", "iag-finance"),
		ServiceClientSecret: os.Getenv("SERVICE_CLIENT_SECRET"),
		AuthTokenURL:        getEnv("AUTH_TOKEN_URL", strings.TrimRight(issuer, "/")+"/oauth/token"),
	}

	return cfg, cfg.validate()
}

func (c Config) validate() error {
	if c.Audience == "" {
		return fmt.Errorf("AUDIENCE is required (e.g. iag.finance)")
	}
	if c.Environment == "production" {
		if c.SeedOnStartup {
			return fmt.Errorf("production must not enable SEED_ON_STARTUP")
		}
		if c.ServiceClientSecret == "" {
			return fmt.Errorf("SERVICE_CLIENT_SECRET is required in production")
		}
	}
	return nil
}

// GinMode returns the gin mode for this environment.
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
