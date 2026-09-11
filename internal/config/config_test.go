package config

import (
	"os"
	"strings"
	"testing"
)

// clearEnv removes every config-relevant environment variable so tests
// don't leak state from the host shell or from each other.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{envDatabaseURL, envRedisURL, envHTTPAddr, envLogLevel, envEnv} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

func TestLoad_MissingRequiredFailsBoot(t *testing.T) {
	clearEnv(t)

	_, err := Load()
	if err == nil {
		t.Fatal("expected an error when DATABASE_URL and REDIS_URL are unset, got nil")
	}
	if !strings.Contains(err.Error(), envDatabaseURL) {
		t.Errorf("expected error to mention %s, got: %v", envDatabaseURL, err)
	}
	if !strings.Contains(err.Error(), envRedisURL) {
		t.Errorf("expected error to mention %s, got: %v", envRedisURL, err)
	}
}

func TestLoad_MissingOnlyDatabaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv(envRedisURL, "redis://localhost:6379/0")

	_, err := Load()
	if err == nil {
		t.Fatal("expected an error when DATABASE_URL is unset, got nil")
	}
	if !strings.Contains(err.Error(), envDatabaseURL) {
		t.Errorf("expected error to mention %s, got: %v", envDatabaseURL, err)
	}
}

func TestLoad_ValidConfigBoots(t *testing.T) {
	clearEnv(t)
	t.Setenv(envDatabaseURL, "postgres://fluxen:fluxen@localhost:5432/fluxen?sslmode=disable")
	t.Setenv(envRedisURL, "redis://localhost:6379/0")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected valid config to load without error, got: %v", err)
	}

	if cfg.DatabaseURL == "" || cfg.RedisURL == "" {
		t.Fatal("expected required fields to be populated")
	}
	// Defaults must be applied when the corresponding env var is unset.
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("expected default HTTPAddr ':8080', got %q", cfg.HTTPAddr)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected default LogLevel 'info', got %q", cfg.LogLevel)
	}
	if cfg.Env != "development" {
		t.Errorf("expected default Env 'development', got %q", cfg.Env)
	}
}

func TestLoad_OverridesRespected(t *testing.T) {
	clearEnv(t)
	t.Setenv(envDatabaseURL, "postgres://fluxen:fluxen@localhost:5432/fluxen?sslmode=disable")
	t.Setenv(envRedisURL, "redis://localhost:6379/0")
	t.Setenv(envHTTPAddr, ":9090")
	t.Setenv(envLogLevel, "debug")
	t.Setenv(envEnv, "production")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTPAddr != ":9090" {
		t.Errorf("expected overridden HTTPAddr ':9090', got %q", cfg.HTTPAddr)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("expected overridden LogLevel 'debug', got %q", cfg.LogLevel)
	}
	if cfg.Env != "production" {
		t.Errorf("expected overridden Env 'production', got %q", cfg.Env)
	}
}
