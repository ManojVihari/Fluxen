// Command fluxen is the combined gateway + control binary and the default
// deployment target (Part B.1/B.4 of the implementation specification).
//
// Phase 1 wires the first real product surface on top of Phase 0's
// foundation: the control API (setup, auth, applications, keys) and the
// OpenAI-compatible gateway (Application → Fluxen Gateway → Provider →
// Response). Everything else — policy/cache/routing, Gemini/Ollama,
// detectors — arrives in later phases.
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

	"fluxen/internal/api"
	"fluxen/internal/auth"
	"fluxen/internal/config"
	"fluxen/internal/gateway"
	"fluxen/internal/health"
	"fluxen/internal/ingest"
	"fluxen/internal/observability"
	"fluxen/internal/store"
	"fluxen/pkg/pricing"
	"fluxen/pkg/providers"
	"fluxen/pkg/providers/openai"
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

	if cfg.OpenAIAPIKey == "" {
		logger.Warn("fluxen: OPENAI_API_KEY is not set — /v1/chat/completions will return 503 no_provider_credential until it is")
	}

	catalog, err := pricing.LoadEmbedded()
	if err != nil {
		return err
	}

	metrics := observability.NewMetrics()

	// --- storage-backed dependencies ---
	apps := store.NewApplications(pool)
	keys := store.NewAPIKeys(pool)
	orgs := store.NewOrganizations(pool)
	users := store.NewUsers(pool)
	requests := store.NewRequests(pool)

	// --- gateway ---
	keyResolver := auth.NewResolver(keys)
	queue := ingest.NewQueue(ingest.DefaultQueueSize, metrics.UsageDropped)
	writer := ingest.NewWriter(queue, requests, logger)

	writerCtx, stopWriter := context.WithCancel(context.Background())
	defer stopWriter() // idempotent; covers every early-return path below
	writerDone := make(chan struct{})
	go func() {
		writer.Run(writerCtx)
		close(writerDone)
	}()

	var provider providers.Provider = openai.NewClient(nil)
	cred := providers.Credential{APIKey: cfg.OpenAIAPIKey, BaseURL: cfg.OpenAIBaseURL}
	gatewaySrv := gateway.NewServer(keyResolver, provider, cred, catalog, queue, logger)

	// --- control API ---
	apiSrv := api.NewServer(api.Server{
		Orgs:            orgs,
		Users:           users,
		Apps:            apps,
		Keys:            keys,
		Sessions:        auth.NewSessionStore(redisClient),
		KeyResolver:     keyResolver,
		DashboardOrigin: cfg.DashboardOrigin,
		Logger:          logger,
	})

	deps := map[string]health.Pinger{
		"postgres": pool,
		"redis":    redisPinger{client: redisClient},
	}
	router := newRouter(deps, metrics, apiSrv.Router(), gatewaySrv.Router())

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

	// Drain in-flight requests before closing the queue (Part B: graceful
	// shutdown flushes the ingest buffer) — srv.Shutdown above already
	// waited for in-flight HTTP handlers to finish, so nothing is still
	// enqueuing by this point.
	stopWriter()
	select {
	case <-writerDone:
	case <-time.After(5 * time.Second):
		logger.Warn("fluxen: ingest writer did not finish flushing within the shutdown grace period")
	}

	logger.Info("fluxen: shutdown complete")
	return nil
}
