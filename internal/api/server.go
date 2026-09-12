// Package api is the control-plane HTTP API the dashboard talks to
// (Part C.1: "control-plane HTTP API (dashboard-facing)"). Phase 1 added
// setup/auth/applications/keys; Phase 2 added the per-application
// summary/timeseries/models rollup reads Application Detail needs; Phase
// 3 added read access to detector output (opportunities); Phase 4 added
// simulations (replay a scenario against real history, read-only,
// production untouched); Phase 5 added the policy editor and the Apply
// action that turns a proven recommendation into a real, enforced
// change; Phase 6 adds measurements — the honest before/after verdict on
// whether an applied change actually worked, and the one-click Revert a
// regression surfaces — everything else (overview, requests, ...)
// arrives in later phases.
package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"fluxen/internal/auth"
	"fluxen/internal/policy"
	"fluxen/internal/store"
	"fluxen/pkg/pricing"
)

// Server holds the control-plane API's dependencies.
type Server struct {
	Orgs          *store.Organizations
	Users         *store.Users
	Apps          *store.Applications
	Keys          *store.APIKeys
	Requests      *store.Requests
	Rollups       *store.Rollups
	Opportunities *store.Opportunities
	Simulations   *store.Simulations
	Measurements  *store.Measurements
	Sessions      *auth.SessionStore

	// Policies is the direct read/write path for GET/PUT policy and
	// history (Part I.4's editor). Applier is the Apply-button
	// transaction (Part G.5) — it wraps Policies plus Opportunities and
	// Simulations, so the handler layer doesn't have to orchestrate that
	// transaction itself.
	Policies *policy.Store
	Applier  *policy.Applier
	// PolicySnapshot is the gateway's in-process cache — invalidated
	// here too (not just by Applier.Apply) so a direct PUT/revert
	// through the editor takes effect immediately, the same as Apply
	// does. Nil-safe: a deployment without a live gateway in this
	// process (or a test) simply skips invalidation.
	PolicySnapshot *policy.Snapshot

	// Catalog prices simulation scenarios via the exact same
	// pkg/pricing.Calculate function the gateway uses (Rule 9) — never a
	// simulation-only reimplementation.
	Catalog *pricing.Catalog

	// KeyResolver is the gateway's own resolver. In Phase 1's combined
	// binary (cmd/fluxen) the API and gateway share one process, so a key
	// revocation can invalidate the gateway's in-process cache entry
	// immediately instead of waiting out its TTL. Nil-safe: a split
	// deployment (cmd/fluxen-control) simply won't have one.
	KeyResolver *auth.Resolver

	// DashboardOrigin is the browser origin allowed to make credentialed
	// cross-origin requests (CORS) — the dashboard runs on a different
	// port than the API in local/dev/default-compose deployments.
	DashboardOrigin string

	Logger *slog.Logger
}

func NewServer(s Server) *Server {
	if s.Logger == nil {
		s.Logger = slog.Default()
	}
	if s.DashboardOrigin == "" {
		s.DashboardOrigin = "http://localhost:3000"
	}
	return &s
}

// Router builds the /api/v1 surface.
func (s *Server) Router() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(s.corsMiddleware)

	r.Route("/api/v1", func(r chi.Router) {
		r.Options("/*", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })

		r.Get("/setup", s.handleSetupStatus)
		r.Post("/setup", s.handleSetup)

		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/logout", s.handleLogout)
		r.Get("/auth/session", s.handleSession)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/applications", s.handleCreateApplication)
			r.Get("/applications", s.handleListApplications)
			r.Get("/applications/{appID}/summary", s.handleApplicationSummary)
			r.Get("/applications/{appID}/timeseries", s.handleApplicationTimeseries)
			r.Get("/applications/{appID}/models", s.handleApplicationModels)
			r.Post("/applications/{appID}/keys", s.handleCreateKey)
			r.Delete("/keys/{keyID}", s.handleRevokeKey)

			r.Get("/opportunities", s.handleListOpportunities)
			r.Get("/opportunities/{opportunityID}", s.handleGetOpportunity)
			r.Post("/opportunities/{opportunityID}/review", s.handleReviewOpportunity)
			r.Post("/opportunities/{opportunityID}/apply", s.handleApplyOpportunity)

			r.Post("/simulations", s.handleCreateSimulation)
			r.Get("/simulations/{simulationID}", s.handleGetSimulation)
			r.Get("/applications/{appID}/simulations", s.handleListApplicationSimulations)

			r.Get("/applications/{appID}/policy", s.handleGetPolicy)
			r.Put("/applications/{appID}/policy", s.handlePutPolicy)
			r.Get("/applications/{appID}/policy/history", s.handlePolicyHistory)
			r.Post("/applications/{appID}/policy/revert", s.handleRevertPolicy)

			r.Get("/measurements", s.handleListMeasurements)
			r.Get("/measurements/{measurementID}", s.handleGetMeasurement)
			r.Post("/measurements/{measurementID}/revert", s.handleRevertMeasurement)
			r.Get("/opportunities/{opportunityID}/measurement", s.handleGetOpportunityMeasurement)
		})
	})

	return r
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", s.DashboardOrigin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		next.ServeHTTP(w, r)
	})
}
