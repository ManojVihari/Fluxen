package gateway

import (
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"fluxen/internal/auth"
	"fluxen/internal/ingest"
	"fluxen/pkg/pricing"
	"fluxen/pkg/providers"
)

// DefaultUpstreamTimeout is applied to every provider call unless the
// caller's context already carries an earlier deadline (Phase 1 backend
// task: "Upstream timeout handling (default 120s)").
const DefaultUpstreamTimeout = 120 * time.Second

// MaxRequestBodyBytes bounds how much of a client's request body Fluxen
// will read before giving up — a safety limit, not a product feature; the
// gateway must never let an unbounded body exhaust memory on the hot path.
const MaxRequestBodyBytes = 25 << 20 // 25MB

// Server holds everything the gateway's HTTP handlers need. It has no
// knowledge of policy, caching, or routing yet — those pipeline stages
// (Part C.5) are wired in starting Phase 5; Phase 1's pipeline is exactly
// authenticate → parse → invoke → account → emit → respond.
type Server struct {
	Resolver   *auth.Resolver
	Provider   providers.Provider
	Credential providers.Credential
	Catalog    *pricing.Catalog
	Queue      *ingest.Queue
	Logger     *slog.Logger
	Timeout    time.Duration
}

// NewServer constructs a Server with Phase 1 defaults filled in for any
// zero-valued optional field.
func NewServer(resolver *auth.Resolver, provider providers.Provider, cred providers.Credential, catalog *pricing.Catalog, queue *ingest.Queue, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		Resolver:   resolver,
		Provider:   provider,
		Credential: cred,
		Catalog:    catalog,
		Queue:      queue,
		Logger:     logger,
		Timeout:    DefaultUpstreamTimeout,
	}
}

// Router builds the gateway's HTTP surface: /v1/chat/completions is the
// only route Phase 1 exposes (Part L Phase 1 API contracts). Auth is
// applied only to gateway routes — /healthz, /readyz, /metrics stay
// unauthenticated and live outside this router (cmd/fluxen wires them
// separately).
func (s *Server) Router() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(RequestIDMiddleware)
	r.Use(AuthMiddleware(s.Resolver))

	r.Post("/v1/chat/completions", s.handleChatCompletions)

	return r
}
