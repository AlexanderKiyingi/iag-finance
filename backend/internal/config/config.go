package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration

	DatabaseURL string
	DataPath    string
	RedisURL    string

	JWTSecret     string
	JWTAccessTTL  time.Duration
	JWTRefreshTTL time.Duration
	JWTRequired   bool

	AllowedOrigins   []string
	AllowDemoReset   bool
	RateLimitPerMin  int
	IsProduction     bool
}

func Load() Config {
	jwtSecret := envStr("JWT_SECRET", "")
	accessMin := envInt("JWT_ACCESS_TTL_MINUTES", 15)
	refreshDays := envInt("JWT_REFRESH_TTL_DAYS", 7)
	dbURL := envStr("DATABASE_URL", "")

	jwtRequired := envBool("JWT_REQUIRED", jwtSecret != "")
	if jwtSecret == "" {
		jwtRequired = false
	}

	origins := envStr("CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://127.0.0.1:3000")
	var allowed []string
	for _, o := range strings.Split(origins, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			allowed = append(allowed, o)
		}
	}

	return Config{
		Port:            envStr("PORT", "8082"),
		ReadTimeout:     time.Duration(envInt("READ_TIMEOUT_SECONDS", 15)) * time.Second,
		WriteTimeout:    time.Duration(envInt("WRITE_TIMEOUT_SECONDS", 15)) * time.Second,
		ShutdownTimeout: time.Duration(envInt("SHUTDOWN_TIMEOUT_SECONDS", 10)) * time.Second,

		DatabaseURL: dbURL,
		DataPath:    envStr("DATA_PATH", "data/finance-state.json"),
		RedisURL:    envStr("REDIS_URL", ""),

		JWTSecret:     jwtSecret,
		JWTAccessTTL:  time.Duration(accessMin) * time.Minute,
		JWTRefreshTTL: time.Duration(refreshDays) * 24 * time.Hour,
		JWTRequired:   jwtRequired,

		AllowedOrigins:  allowed,
		AllowDemoReset:  envBool("ALLOW_DEMO_RESET", !envBool("GO_ENV", false) && os.Getenv("GIN_MODE") != "release"),
		RateLimitPerMin: envInt("RATE_LIMIT_PER_MINUTE", 300),
		IsProduction:    os.Getenv("GO_ENV") == "production" || os.Getenv("GIN_MODE") == "release",
	}
}

func (c Config) PostgresEnabled() bool  { return c.DatabaseURL != "" }
func (c Config) RedisEnabled() bool     { return c.RedisURL != "" }
func (c Config) JWTEnabled() bool       { return c.JWTSecret != "" }

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
