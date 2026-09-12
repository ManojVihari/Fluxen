// Command fluxen is the combined gateway + control binary and the default
// deployment target (Part B.1/B.4 of the implementation specification).
//
// Phase 1 added the control API (setup, auth, applications, keys) and the
// OpenAI-compatible gateway. Phase 2 added the in-process job scheduler
// (Part C.7) running the rollup.hourly/rollup.daily jobs that turn raw
// requests into the aggregates Application Detail reads. Phase 3 added
// the detect.model_cost job — the Aha Moment: real traffic in, a
// credible optimization opportunity out. Phase 5 wired the five control
// surfaces (model routing, exact caching, budget, rate limit, model
// restriction) into the live gateway pipeline for real, replacing the
// no-op stages every earlier phase ran with. Phase 6 adds the
// measure.check job — interim (+7d) and final (+14d) checks that close
// the loop between what an applied change was estimated to save and
// what it actually did. Gemini/Ollama and the other three detectors
// arrive in later phases.
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
	"fluxen/internal/cache"
	"fluxen/internal/config"
	"fluxen/internal/credentials"
	"fluxen/internal/detect"
	"fluxen/internal/gateway"
	"fluxen/internal/guard"
	"fluxen/internal/health"
	"fluxen/internal/ingest"
	"fluxen/internal/measure"
	"fluxen/internal/observability"
	"fluxen/internal/policy"
	"fluxen/internal/retention"
	"fluxen/internal/rollup"
	"fluxen/internal/score"
	"fluxen/internal/store"
	"fluxen/internal/worker"
	"fluxen/pkg/pricing"
	"fluxen/pkg/providers"
	"fluxen/pkg/providers/gemini"
	"fluxen/pkg/providers/ollama"
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
	simulations := store.NewSimulations(pool)
	measurements := store.NewMeasurements(pool)
	scores := store.NewScores(pool)
	overview := store.NewOverview(pool)
	policyStore := policy.NewStore(pool)
	providerCredsStore := store.NewProviderCredentials(pool)
	retentionSettingsSnapshot := retention.NewSnapshot(orgs)

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

	// --- policy (Part L Phase 5) ---
	policySnapshot := policy.NewSnapshot(policyStore, redisClient, logger)
	snapshotCtx, stopSnapshotSubscribe := context.WithCancel(context.Background())
	defer stopSnapshotSubscribe() // idempotent; covers every early-return path below
	go policySnapshot.Subscribe(snapshotCtx)

	rateLimiter := guard.NewRateLimiter(redisClient, logger)
	budgetGuard := guard.NewBudgetGuard(redisClient, logger)
	cacheStore := cache.NewStore(redisClient, logger)

	applier := &policy.Applier{
		Policies: policyStore, Opportunities: opportunities, Simulations: simulations, Snapshot: policySnapshot,
		Measurer: measure.NewFreezer(requests, measurements),
	}

	var provider providers.Provider = openai.NewClient(nil)
	cred := providers.Credential{APIKey: cfg.OpenAIAPIKey, BaseURL: cfg.OpenAIBaseURL}

	// Gemini/Ollama client instances always exist — they're stateless
	// HTTP clients, so building them costs nothing, and Settings >
	// Providers (backed by the DB, not an env var) needs a live client to
	// call Models/Health on regardless of whether the static env-var
	// credentials below are ever set. extraCredentials stays the static,
	// env-var-only fallback: a deployment with neither GEMINI_API_KEY nor
	// OLLAMA_BASE_URL set, and no DB-managed credential either, behaves
	// exactly like Phase 1-6's OpenAI-only gateway for those providers
	// (Server.resolveProvider finds no credential from any source and
	// 503s, same as today).
	extraProviders := map[string]providers.Provider{
		"gemini": gemini.NewClient(nil),
		"ollama": ollama.NewClient(nil),
	}
	extraCredentials := map[string]providers.Credential{}
	if cfg.GeminiAPIKey != "" {
		extraCredentials["gemini"] = providers.Credential{APIKey: cfg.GeminiAPIKey}
	}
	if cfg.OllamaBaseURL != "" {
		extraCredentials["ollama"] = providers.Credential{BaseURL: cfg.OllamaBaseURL}
	}

	// Provider clients exist regardless of which credential source is
	// used (env var or the encrypted provider_credentials table below) —
	// they're stateless HTTP clients, so the same three instances serve
	// both the gateway's Chat/ChatStream calls and Settings > Providers'
	// Models/Health calls.
	allProviderClients := map[string]providers.Provider{"openai": provider}
	for name, p := range extraProviders {
		allProviderClients[name] = p
	}

	// Org-managed, encrypted provider credentials (Part E.1). This is
	// what makes `docker compose up` with zero configuration produce a
	// deployment where Settings > Providers just works: no env var is
	// required — resolveEncryptionKey generates and persists a key to
	// cfg.DataDir on first boot if FLUXEN_ENCRYPTION_KEY was never set.
	credentialBox, err := resolveEncryptionKey(cfg.EncryptionKey, cfg.DataDir, logger)
	if err != nil {
		return err
	}
	credentialService := credentials.NewService(providerCredsStore, credentialBox)
	credentialSnapshot := credentials.NewSnapshot(credentialService, redisClient, logger)
	credSnapshotCtx, stopCredSnapshotSubscribe := context.WithCancel(context.Background())
	defer stopCredSnapshotSubscribe() // idempotent; covers every early-return path below
	go credentialSnapshot.Subscribe(credSnapshotCtx)

	gatewaySrv := gateway.NewServer(gateway.Server{
		Resolver: keyResolver, Provider: provider, Credential: cred, Catalog: catalog, Queue: queue,
		Providers: extraProviders, Credentials: extraCredentials, CredentialSnapshot: credentialSnapshot,
		RetentionSettings: retentionSettingsSnapshot,
		Policy:            policySnapshot, RateLimiter: rateLimiter, BudgetGuard: budgetGuard, Cache: cacheStore,
		Logger: logger,
	})

	// --- control API ---
	apiSrv := api.NewServer(api.Server{
		Orgs:               orgs,
		Users:              users,
		Apps:               apps,
		Keys:               keys,
		Requests:           requests,
		Rollups:            rollups,
		Opportunities:      opportunities,
		Simulations:        simulations,
		Measurements:       measurements,
		Scores:             scores,
		Overview:           overview,
		Credentials:        credentialService,
		CredentialSnapshot: credentialSnapshot,
		ProviderClients:    allProviderClients,
		Policies:           policyStore,
		Applier:            applier,
		PolicySnapshot:     policySnapshot,
		Catalog:            catalog,
		Sessions:           auth.NewSessionStore(redisClient),
		LoginLimiter:       auth.NewLoginLimiter(redisClient, logger),
		KeyResolver:        keyResolver,
		DashboardOrigin:    cfg.DashboardOrigin,
		CookieSecure:       cfg.CookieSecure,
		Logger:             logger,
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
		// Part C.7's frozen job table: "detect.run | every 6h per app |
		// run all four detectors" — renamed from this job's earlier
		// Phase-3 name/cadence ("detect.model_cost" every 15m) now that
		// all four detectors actually run here, not just Model Cost.
		Name:     "detect.run",
		Interval: 6 * time.Hour,
		Run:      detectRunner.Run,
	})
	measureRunner := measure.NewRunner(measurements, opportunities, requests, policyStore, logger)
	scheduler.Register(worker.Job{
		Name:     "measure.check",
		Interval: time.Hour,
		Run:      measureRunner.Run,
	})
	scoreRunner := score.NewRunner(apps, rollups, requests, scores, catalog, logger)
	scheduler.Register(worker.Job{
		Name:     "score.daily",
		Interval: 24 * time.Hour,
		Run:      scoreRunner.Run,
	})
	retentionRunner := retention.NewRunner(orgs, store.NewRetention(pool), logger)
	scheduler.Register(worker.Job{
		Name:     "retention.enforce",
		Interval: 24 * time.Hour,
		Run:      retentionRunner.Run,
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
		// ReadHeaderTimeout mitigates slow-header (Slowloris-style) DoS —
		// a client that opens a connection and trickles headers in never
		// ties one up indefinitely. IdleTimeout bounds how long a
		// keep-alive connection sits idle. Deliberately no blanket
		// ReadTimeout/WriteTimeout: streaming chat completions (SSE) can
		// legitimately run for minutes, and a fixed WriteTimeout would
		// truncate them mid-stream.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
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
