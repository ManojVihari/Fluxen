// Package api is the control-plane HTTP API the dashboard talks to
// (Part C.1: "control-plane HTTP API (dashboard-facing)"). Phase 1 added
// setup/auth/applications/keys; Phase 2 adds the per-application
// summary/timeseries/models rollup reads Application Detail needs —
// everything else (overview, requests, opportunities, ...) arrives in
// later phases.
package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"fluxen/internal/auth"
	"fluxen/internal/store"
)

// Server holds the control-plane API's dependencies.
type Server struct {
	Orgs     *store.Organizations
	Users    *store.Users
	Apps     *store.Applications
	Keys     *store.APIKeys
	Rollups  *store.Rollups
	Sessions *auth.SessionStore

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
		})
	})

	return r
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", s.DashboardOrigin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		next.ServeHTTP(w, r)
	})
}
