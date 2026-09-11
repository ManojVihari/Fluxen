// Package config loads and validates Fluxen's process configuration from
// environment variables. Config is read once at boot; a missing required
// value fails startup immediately with a clear error rather than surfacing
// later as a confusing runtime failure.
package config

import (
	"fmt"
	"os"
	"strings"
)

// Config holds the environment-derived settings for the combined
// fluxen (gateway + control) binary. Phase 0 only needs enough to boot the
// process and connect to its two datastores; later phases extend this
// struct as their own tasks require new settings.
type Config struct {
	// Env is a free-form deployment label ("development", "production", ...).
	// It has no behavioral effect in Phase 0 beyond appearing in logs.
	Env string

	// HTTPAddr is the address the combined HTTP server listens on.
	HTTPAddr string

	// LogLevel controls the minimum slog level emitted ("debug", "info",
	// "warn", "error").
	LogLevel string

	// DatabaseURL is a standard PostgreSQL connection string
	// (e.g. postgres://user:pass@host:5432/db?sslmode=disable). Required.
	DatabaseURL string

	// RedisURL is a standard Redis connection string
	// (e.g. redis://host:6379/0). Required.
	RedisURL string
}

const (
	envDatabaseURL = "DATABASE_URL"
	envRedisURL    = "REDIS_URL"
	envHTTPAddr    = "HTTP_ADDR"
	envLogLevel    = "LOG_LEVEL"
	envEnv         = "FLUXEN_ENV"
)

// Load reads configuration from the process environment and validates it.
// It returns an error naming every missing required variable at once,
// rather than failing on the first one, so a misconfigured deployment can
// be fixed in a single pass.
func Load() (*Config, error) {
	cfg := &Config{
		Env:         getOrDefault(envEnv, "development"),
		HTTPAddr:    getOrDefault(envHTTPAddr, ":8080"),
		LogLevel:    getOrDefault(envLogLevel, "info"),
		DatabaseURL: os.Getenv(envDatabaseURL),
		RedisURL:    os.Getenv(envRedisURL),
	}

	var missing []string
	if cfg.DatabaseURL == "" {
		missing = append(missing, envDatabaseURL)
	}
	if cfg.RedisURL == "" {
		missing = append(missing, envRedisURL)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("config: missing required environment variable(s): %s", strings.Join(missing, ", "))
	}

	return cfg, nil
}

func getOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
