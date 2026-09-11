package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/redis/go-redis/v9"

	"fluxen/internal/config"
	"fluxen/internal/health"
	"fluxen/internal/observability"
	"fluxen/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

// bootstrap opens the Postgres pool and Redis client, applies pending
// migrations, and returns everything main needs to serve traffic plus a
// cleanup func that closes both connections. Splitting this out of run()
// is what lets the Phase 0 boot smoke test (app_test.go) exercise the real
// startup sequence — pool open, migrate, redis connect — against
// testcontainer-provided Postgres/Redis without also standing up a real
// OS-signal-driven server loop.
func bootstrap(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, *redis.Client, func(), error) {
	pool, err := store.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, nil, err
	}

	sqlDB, err := store.OpenSQLDB(cfg.DatabaseURL)
	if err != nil {
		pool.Close()
		return nil, nil, nil, err
	}
	defer sqlDB.Close()

	if err := store.MigrateUp(sqlDB); err != nil {
		pool.Close()
		return nil, nil, nil, err
	}

	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		pool.Close()
		return nil, nil, nil, err
	}
	redisClient := redis.NewClient(redisOpts)

	if err := redisClient.Ping(ctx).Err(); err != nil {
		pool.Close()
		_ = redisClient.Close()
		return nil, nil, nil, err
	}

	cleanup := func() {
		pool.Close()
		_ = redisClient.Close()
	}
	return pool, redisClient, cleanup, nil
}

// redisPinger adapts *redis.Client to internal/health.Pinger — the
// go-redis client's Ping returns a *redis.StatusCmd rather than a plain
// error, so it needs a one-line adapter to satisfy the shared interface.
type redisPinger struct{ client *redis.Client }

func (r redisPinger) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

// newRouter builds the Phase 0 HTTP surface: /healthz (liveness),
// /readyz (readiness — checks deps), /metrics (Prometheus). No gateway
// pipeline and no control API exist yet.
func newRouter(deps map[string]health.Pinger, metrics *observability.Metrics) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		checkCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		result := health.Check(checkCtx, deps)
		status := map[string]string{}
		for name, s := range result {
			if s.OK {
				status[name] = "ok"
			} else {
				status[name] = "error: " + s.Error
			}
		}

		code := http.StatusOK
		if !health.AllOK(result) {
			code = http.StatusServiceUnavailable
		}
		writeJSON(w, code, status)
	})

	r.Handle("/metrics", metrics.Handler())

	return r
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
