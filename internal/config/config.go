package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alvor-technologies/iag-platform-go/corsenv"
)

// Config holds runtime knobs for the finance service.
type Config struct {
	ServiceName       string
	Environment       string
	BaseCurrency      string
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
	EnableEventPublish bool
	KafkaBrokers      []string
	KafkaClientID     string
	KafkaGroupID      string
	KafkaTopic            string
	KafkaSupplyChainTopic string
	KafkaCommercialTopic    string
	KafkaOperationsTopic    string
	KafkaNotificationsTopic string
	KafkaDLQTopic         string
	ShutdownTimeout   time.Duration
	ReadHeaderTimeout time.Duration

	// Outbound service-account credentials.
	ServiceClientID     string
	ServiceClientSecret string
	AuthTokenURL        string

	// URA EFRIS adapter (HTTP when URA_EFRIS_BASE_URL set; ura_s2s for live URA).
	EFRISMode      string
	EFRISBaseURL   string
	EFRISAPIKey    string
	EFRISTIN       string
	EFRISSimulate  bool
	EFRISS2SURL    string
	EFRISS2SPath   string
	EFRISDeviceNo  string
	EFRISBranchID  string
	EFRISAESKey    string

	// Users service (billing identity resolution).
	UsersAPIURL string

	// Customer-facing AR payment links.
	PaymentLinkBaseURL string

	// Bank feed adapter (HTTP when BANK_FEED_BASE_URL set).
	BankFeedBaseURL   string
	BankFeedAPIKey    string
	BankFeedProvider  string
	BankFeedSimulate  bool

	// Overdue AR notification cron.
	OverdueCronEnabled   bool
	OverdueCronInterval  time.Duration
	OverdueNotifyEmail   string
	OverdueNotifyHref    string
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

	corsRaw := corsenv.Allowlist(corsenv.DefaultDevOrigins)
	var corsOrigins []string
	for _, p := range strings.Split(corsRaw, ",") {
		if s := strings.TrimSpace(p); s != "" {
			corsOrigins = append(corsOrigins, s)
		}
	}

	cfg := Config{
		ServiceName:         getEnv("SERVICE_NAME", "finance"),
		BaseCurrency:        strings.ToUpper(getEnv("BASE_CURRENCY", "UGX")),
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
		EnableConsumer:       getEnv("ENABLE_CONSUMER", defaultConsumer(env)) == "true",
		EnableEventPublish:   getEnv("ENABLE_EVENT_PUBLISH", "true") == "true",
		KafkaBrokers:         splitBrokers(getEnv("KAFKA_BROKERS", "localhost:19092")),
		KafkaClientID:       getEnv("KAFKA_CLIENT_ID", "finance"),
		KafkaGroupID:        getEnv("KAFKA_GROUP_ID", "iag.finance.ledger"),
		KafkaTopic:            getEnv("KAFKA_FINANCE_TOPIC", "iag.finance"),
		KafkaSupplyChainTopic:   getEnv("KAFKA_SUPPLY_CHAIN_TOPIC", "iag.supply-chain"),
		KafkaCommercialTopic:    getEnv("KAFKA_COMMERCIAL_TOPIC", "iag.commercial"),
		KafkaOperationsTopic:  getEnv("KAFKA_OPERATIONS_TOPIC", "iag.operations"),
		KafkaNotificationsTopic: getEnv("KAFKA_NOTIFICATIONS_TOPIC", "iag.notifications"),
		KafkaDLQTopic:           getEnv("KAFKA_DLQ_TOPIC", "iag.finance.dlq"),
		ShutdownTimeout:     time.Duration(shutdownSec) * time.Second,
		ReadHeaderTimeout:   10 * time.Second,
		ServiceClientID:     getEnv("SERVICE_CLIENT_ID", "iag-finance"),
		ServiceClientSecret: os.Getenv("SERVICE_CLIENT_SECRET"),
		AuthTokenURL:        getEnv("AUTH_TOKEN_URL", strings.TrimRight(issuer, "/")+"/oauth/token"),
		EFRISMode:           strings.ToLower(strings.TrimSpace(os.Getenv("URA_EFRIS_MODE"))),
		EFRISBaseURL:        strings.TrimSpace(os.Getenv("URA_EFRIS_BASE_URL")),
		EFRISAPIKey:         strings.TrimSpace(os.Getenv("URA_EFRIS_API_KEY")),
		EFRISTIN:            strings.TrimSpace(os.Getenv("URA_EFRIS_TIN")),
		EFRISSimulate:       getEnv("URA_EFRIS_SIMULATE", "false") == "true",
		EFRISS2SURL:         strings.TrimSpace(os.Getenv("URA_EFRIS_S2S_URL")),
		EFRISS2SPath:        strings.TrimSpace(os.Getenv("URA_EFRIS_S2S_PATH")),
		EFRISDeviceNo:       strings.TrimSpace(os.Getenv("URA_EFRIS_DEVICE_NO")),
		EFRISBranchID:       strings.TrimSpace(os.Getenv("URA_EFRIS_BRANCH_ID")),
		EFRISAESKey:         strings.TrimSpace(os.Getenv("URA_EFRIS_AES_KEY")),
		UsersAPIURL:         strings.TrimSpace(firstNonEmpty(os.Getenv("USERS_API_URL"), os.Getenv("ACCOUNTS_URL"))),
		PaymentLinkBaseURL:  strings.TrimSpace(os.Getenv("PAYMENT_LINK_BASE_URL")),
		BankFeedBaseURL:     strings.TrimSpace(os.Getenv("BANK_FEED_BASE_URL")),
		BankFeedAPIKey:      strings.TrimSpace(os.Getenv("BANK_FEED_API_KEY")),
		BankFeedProvider:    getEnv("BANK_FEED_PROVIDER", "stanbic"),
		BankFeedSimulate:    getEnv("BANK_FEED_SIMULATE", "false") == "true",
		OverdueCronEnabled:  getEnv("OVERDUE_CRON_ENABLED", overdueCronDefault(env)) == "true",
		OverdueCronInterval: overdueCronInterval(),
		OverdueNotifyEmail:  strings.TrimSpace(os.Getenv("OVERDUE_NOTIFY_EMAIL")),
		OverdueNotifyHref:   strings.TrimSpace(os.Getenv("OVERDUE_NOTIFY_HREF")),
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
	// Fail fast when a live EFRIS mode is selected without its credentials,
	// rather than discovering the gap mid-submission to the tax authority.
	switch c.EFRISMode {
	case "http":
		if c.EFRISBaseURL == "" || c.EFRISAPIKey == "" || c.EFRISTIN == "" {
			return fmt.Errorf("URA_EFRIS_MODE=http requires URA_EFRIS_BASE_URL, URA_EFRIS_API_KEY and URA_EFRIS_TIN")
		}
	case "ura_s2s":
		if c.EFRISS2SURL == "" || c.EFRISAESKey == "" || c.EFRISTIN == "" {
			return fmt.Errorf("URA_EFRIS_MODE=ura_s2s requires URA_EFRIS_S2S_URL, URA_EFRIS_AES_KEY and URA_EFRIS_TIN")
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

func overdueCronDefault(env string) string {
	if env == "production" || env == "staging" {
		return "true"
	}
	return "false"
}

func overdueCronInterval() time.Duration {
	min, err := strconv.Atoi(getEnv("OVERDUE_CRON_INTERVAL_MINUTES", "1440"))
	if err != nil || min < 5 {
		min = 1440
	}
	return time.Duration(min) * time.Minute
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
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
