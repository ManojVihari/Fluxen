// Command fluxen is the combined gateway + control binary and the default
// deployment target (Part B.1/B.4 of the implementation specification).
//
// Phase 0 gives it exactly three responsibilities: load config, connect to
// Postgres and Redis (applying pending migrations along the way), and serve
// /healthz, /readyz, and /metrics. No gateway pipeline, no control API, and
// no business logic exist yet — those are built starting in Phase 1.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fluxen/internal/config"
	"fluxen/internal/health"
	"fluxen/internal/observability"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fluxen: fatal startup error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := observability.NewLogger(cfg.LogLevel)
	slog.SetDefault(logger)
	logger.Info("fluxen: starting", "env", cfg.Env, "addr", cfg.HTTPAddr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("fluxen: connecting to postgres and redis, applying migrations")
	pool, redisClient, cleanup, err := bootstrap(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()
	logger.Info("fluxen: postgres and redis ready")

	metrics := observability.NewMetrics()
	deps := map[string]health.Pinger{
		"postgres": pool,
		"redis":    redisPinger{client: redisClient},
	}
	router := newRouter(deps, metrics)

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: router,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("fluxen: http server listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("fluxen: shutdown signal received")
	case err := <-serveErr:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	logger.Info("fluxen: shutdown complete")
	return nil
}
