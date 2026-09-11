package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

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

// newRouter builds Fluxen's full HTTP surface. /healthz, /readyz, and
// /metrics are the operational endpoints (Phase 0); /api/ and /v1/ are
// mounted separately as apiHandler (internal/api's control-plane API) and
// gatewayHandler (internal/gateway's OpenAI-compatible ingress) so this
// function stays free of any gateway/api-specific logic — it only routes
// by prefix. Either handler may be nil (tests that only exercise the
// Phase 0 operational endpoints pass nil, nil).
func newRouter(deps map[string]health.Pinger, metrics *observability.Metrics, apiHandler, gatewayHandler http.Handler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
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

	mux.Handle("/metrics", metrics.Handler())

	if apiHandler != nil {
		mux.Handle("/api/", apiHandler)
	}
	if gatewayHandler != nil {
		mux.Handle("/v1/", gatewayHandler)
	}

	return mux
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
