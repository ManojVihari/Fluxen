package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"

	"fluxen/internal/config"
	"fluxen/internal/health"
	"fluxen/internal/observability"
)

// TestBoot_ReadyzReflectsPostgresAndRedis is the Phase 0 boot smoke test
// called for by the specification's Phase 0 Tests section: exercise the
// real bootstrap sequence (open pool, apply migrations, connect Redis)
// against real, disposable Postgres and Redis containers, then assert
// /readyz reports both as healthy.
//
// It requires a working Docker daemon and is skipped automatically when
// one isn't available, so `go test ./...` stays runnable in environments
// without Docker.
func TestBoot_ReadyzReflectsPostgresAndRedis(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-based boot smoke test in -short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pgContainer, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("fluxen"),
		tcpostgres.WithUsername("fluxen"),
		tcpostgres.WithPassword("fluxen"),
		// Postgres logs its "ready" message once, restarts itself to apply
		// init-time config, then logs it again — waiting for only the first
		// occurrence (or just the open port) races the real startup and
		// produces exactly the "connection reset by peer" failure this
		// wait strategy avoids.
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Skipf("skipping: could not start postgres testcontainer (is Docker running?): %v", err)
	}
	t.Cleanup(func() { _ = pgContainer.Terminate(ctx) })

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get postgres connection string: %v", err)
	}

	redisContainer, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Skipf("skipping: could not start redis testcontainer (is Docker running?): %v", err)
	}
	t.Cleanup(func() { _ = redisContainer.Terminate(ctx) })

	redisURL, err := redisContainer.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get redis connection string: %v", err)
	}

	cfg := &config.Config{
		Env:         "test",
		HTTPAddr:    ":0",
		LogLevel:    "error",
		DatabaseURL: dsn,
		RedisURL:    redisURL,
	}

	pool, redisClient, cleanup, err := bootstrap(ctx, cfg)
	if err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	defer cleanup()

	metrics := observability.NewMetrics()
	deps := map[string]health.Pinger{
		"postgres": pool,
		"redis":    redisPinger{client: redisClient},
	}
	router := newRouter(deps, metrics)

	ts := httptest.NewServer(router)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 from /readyz with healthy deps, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode /readyz body: %v", err)
	}
	if body["postgres"] != "ok" {
		t.Errorf("expected postgres=ok in /readyz body, got: %+v", body)
	}
	if body["redis"] != "ok" {
		t.Errorf("expected redis=ok in /readyz body, got: %+v", body)
	}
}

// TestHealthz_AlwaysOKRegardlessOfDeps confirms /healthz is a pure liveness
// probe: it never inspects dependencies, so it stays 200 even with an empty
// dependency map (Part C.5 / cmd/fluxen main.go doc comment).
func TestHealthz_AlwaysOKRegardlessOfDeps(t *testing.T) {
	metrics := observability.NewMetrics()
	router := newRouter(map[string]health.Pinger{}, metrics)

	ts := httptest.NewServer(router)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 from /healthz, got %d", resp.StatusCode)
	}
}
