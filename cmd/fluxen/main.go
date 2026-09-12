// Command fluxen is the combined gateway + control binary and the default
// deployment target (Part B.1/B.4 of the implementation specification).
//
// Phase 1 added the control API (setup, auth, applications, keys) and the
// OpenAI-compatible gateway. Phase 2 added the in-process job scheduler
// (Part C.7) running the rollup.hourly/rollup.daily jobs that turn raw
// requests into the aggregates Application Detail reads. Phase 3 adds the
// detect.model_cost job — the Aha Moment: real traffic in, a credible
// optimization opportunity out. Everything else — policy/cache/routing,
// Gemini/Ollama, the other three detectors — arrives in later phases.
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
	"fluxen/internal/detect"
	"fluxen/internal/gateway"
	"fluxen/internal/health"
	"fluxen/internal/ingest"
	"fluxen/internal/observability"
	"fluxen/internal/rollup"
	"fluxen/internal/store"
	"fluxen/internal/worker"
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
	rollups := store.NewRollups(pool)
	opportunities := store.NewOpportunities(pool)

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
		Rollups:         rollups,
		Opportunities:   opportunities,
		Sessions:        auth.NewSessionStore(redisClient),
		KeyResolver:     keyResolver,
		DashboardOrigin: cfg.DashboardOrigin,
		Logger:          logger,
	})

	// --- background jobs (Part C.7) ---
	scheduler := worker.NewScheduler(logger)
	scheduler.Register(worker.Job{
		Name:     "rollup.hourly",
		Interval: 5 * time.Minute,
		Run: func(ctx context.Context) error {
			from, to := rollup.HourlyWindow(time.Now())
			return rollup.ComputeHourly(ctx, pool, from, to)
		},
	})
	scheduler.Register(worker.Job{
		Name:     "rollup.daily",
		Interval: time.Hour,
		Run: func(ctx context.Context) error {
			from, to := rollup.DailyWindow(time.Now())
			return rollup.ComputeDaily(ctx, pool, from, to)
		},
	})
	detectRunner := detect.NewRunner(apps, rollups, requests, opportunities, catalog, logger)
	scheduler.Register(worker.Job{
		Name:     "detect.model_cost",
		Interval: 15 * time.Minute,
		Run:      detectRunner.Run,
	})

	schedulerCtx, stopScheduler := context.WithCancel(context.Background())
	defer stopScheduler() // idempotent; covers every early-return path below
	schedulerDone := make(chan struct{})
	go func() {
		scheduler.Run(schedulerCtx)
		close(schedulerDone)
	}()

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

	stopScheduler()
	select {
	case <-schedulerDone:
	case <-time.After(5 * time.Second):
		logger.Warn("fluxen: background job scheduler did not stop within the shutdown grace period")
	}

	logger.Info("fluxen: shutdown complete")
	return nil
}
