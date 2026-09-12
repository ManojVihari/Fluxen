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
// fluxen (gateway + control) binary.
type Config struct {
	// Env is a free-form deployment label ("development", "production", ...).
	// It has no behavioral effect beyond appearing in logs.
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

	// OpenAIAPIKey is the deployment-wide OpenAI credential the gateway
	// uses to serve proxied traffic. It is optional at boot — a
	// deployment can start without it — but every /v1/chat/completions
	// call fails with a clear 503 no_provider_credential until it's set.
	//
	// This is a deliberate Phase 1 simplification: Fluxen's real
	// provider-credential model is org-scoped, encrypted, and
	// UI-managed (provider_credentials, Part E.1) — that table and its
	// CRUD API are a Phase 7 task ("Complete V1 Product"), not a Phase 1
	// one. Pulling that table forward just to avoid an env var would
	// violate the phase boundary in the other direction. A single env
	// var is the minimal way to prove the gateway's real end-to-end path
	// (Application → Fluxen Gateway → Provider → Response) without
	// building Phase 7 early.
	OpenAIAPIKey string

	// OpenAIBaseURL overrides the OpenAI API base URL. Empty means the
	// real https://api.openai.com — this exists for pointing the gateway
	// at a mock server in tests and local development, not as a product
	// feature.
	OpenAIBaseURL string

	// GeminiAPIKey/OllamaBaseURL are the same Phase-1-style deployment-
	// wide simplification as OpenAIAPIKey above, extended to the two
	// providers Phase 7 adds. Real per-org, encrypted, UI-managed
	// provider_credentials (Part E.1) remain a documented gap this pass
	// does not close — see the Phase 7 completion notes — but a
	// deployment can still proxy real Gemini and Ollama traffic today via
	// these env vars, the same way Phase 1 shipped OpenAI support before
	// its own credentials table existed.
	GeminiAPIKey string
	// OllamaBaseURL points at a self-hosted Ollama instance (e.g.
	// http://localhost:11434, or a Docker Compose service name). Ollama
	// support is only enabled when this is set — unlike OpenAI/Gemini,
	// there is no public default endpoint to fall back to.
	OllamaBaseURL string

	// EncryptionKey is a base64-encoded 32-byte AES-256 key (e.g.
	// `openssl rand -base64 32`) used to encrypt provider_credentials at
	// rest (Part E.1). Entirely optional: a deployment that never sets it
	// isn't left without encrypted credential storage the way this field's
	// name might suggest — cmd/fluxen's own resolveEncryptionKey generates
	// and persists one to DataDir on first boot instead. Setting this env
	// var only matters for an operator who wants to manage the key
	// themselves (a secrets manager, a value injected by their own
	// orchestration) rather than let Fluxen generate and store it.
	EncryptionKey string

	// DataDir is where cmd/fluxen persists small local state that isn't a
	// fit for Postgres — today, just the generated encryption key above.
	// docker-compose.yml backs this with its own named volume, separate
	// from the Postgres volume the encrypted data itself lives in.
	DataDir string

	// DashboardOrigin is the browser origin the control API allows to
	// make credentialed cross-origin requests (CORS) — the dashboard
	// runs on a different port than the API in the default compose
	// deployment.
	DashboardOrigin string

	// CookieSecure sets the session cookie's Secure flag. Defaults to
	// false so the default compose deployment (plain HTTP) works out of
	// the box; an operator who puts TLS in front of Fluxen should set
	// FLUXEN_COOKIE_SECURE=true.
	CookieSecure bool
}

const (
	envDatabaseURL     = "DATABASE_URL"
	envRedisURL        = "REDIS_URL"
	envHTTPAddr        = "HTTP_ADDR"
	envLogLevel        = "LOG_LEVEL"
	envEnv             = "FLUXEN_ENV"
	envOpenAIAPIKey    = "OPENAI_API_KEY"
	envOpenAIBaseURL   = "FLUXEN_OPENAI_BASE_URL"
	envGeminiAPIKey    = "GEMINI_API_KEY"
	envOllamaBaseURL   = "OLLAMA_BASE_URL"
	envEncryptionKey   = "FLUXEN_ENCRYPTION_KEY"
	envDataDir         = "FLUXEN_DATA_DIR"
	envDashboardOrigin = "FLUXEN_DASHBOARD_ORIGIN"
	envCookieSecure    = "FLUXEN_COOKIE_SECURE"
)

// Load reads configuration from the process environment and validates it.
// It returns an error naming every missing required variable at once,
// rather than failing on the first one, so a misconfigured deployment can
// be fixed in a single pass.
func Load() (*Config, error) {
	cfg := &Config{
		Env:             getOrDefault(envEnv, "development"),
		HTTPAddr:        getOrDefault(envHTTPAddr, ":8080"),
		LogLevel:        getOrDefault(envLogLevel, "info"),
		DatabaseURL:     os.Getenv(envDatabaseURL),
		RedisURL:        os.Getenv(envRedisURL),
		OpenAIAPIKey:    os.Getenv(envOpenAIAPIKey),
		OpenAIBaseURL:   os.Getenv(envOpenAIBaseURL),
		GeminiAPIKey:    os.Getenv(envGeminiAPIKey),
		OllamaBaseURL:   os.Getenv(envOllamaBaseURL),
		EncryptionKey:   os.Getenv(envEncryptionKey),
		DataDir:         getOrDefault(envDataDir, "/data"),
		DashboardOrigin: getOrDefault(envDashboardOrigin, "http://localhost:3000"),
		CookieSecure:    os.Getenv(envCookieSecure) == "true",
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
