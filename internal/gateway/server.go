package gateway

import (
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"fluxen/internal/auth"
	"fluxen/internal/cache"
	"fluxen/internal/guard"
	"fluxen/internal/ingest"
	"fluxen/internal/policy"
	corepolicy "fluxen/pkg/policy"
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

// Server holds everything the gateway's HTTP handlers need. Phase 1's
// pipeline was exactly authenticate → parse → invoke → account → emit →
// respond; Phase 5 inserts policy load, model-restriction/rate-limit/
// budget checks, routing, and cache lookup/store between parse and
// invoke (Part C.5) — Policy/RateLimiter/BudgetGuard/Cache below are all
// nil-safe: a Server built without them (e.g. in a test that doesn't
// care about Phase 5) behaves exactly like Phase 1's pipeline.
type Server struct {
	Resolver    *auth.Resolver
	Provider    providers.Provider
	Credential  providers.Credential
	Catalog     *pricing.Catalog
	Queue       *ingest.Queue
	Policy      *policy.Snapshot
	RateLimiter *guard.RateLimiter
	BudgetGuard *guard.BudgetGuard
	Cache       *cache.Store
	Logger      *slog.Logger
	Timeout     time.Duration

	rng *rand.Rand
}

// safeSource wraps a rand.Source behind a mutex — math/rand's default
// sources are not safe for concurrent use, and the gateway's hot path is
// inherently concurrent. Wrapping the Source (not the *rand.Rand itself)
// means the resulting *rand.Rand is a plain value Server can hold and
// pass straight to pkg/policy.Evaluate with no locking at the call site.
type safeSource struct {
	mu  sync.Mutex
	src rand.Source
}

func (s *safeSource) Int63() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.src.Int63()
}

func (s *safeSource) Seed(seed int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.src.Seed(seed)
}

// NewServer constructs a Server with Phase 1 defaults filled in for any
// zero-valued optional field.
func NewServer(s Server) *Server {
	if s.Logger == nil {
		s.Logger = slog.Default()
	}
	if s.Timeout == 0 {
		s.Timeout = DefaultUpstreamTimeout
	}
	s.rng = rand.New(&safeSource{src: rand.NewSource(time.Now().UnixNano())})
	return &s
}

// evaluatePolicy calls pkg/policy.Evaluate — the pure decision function
// production and internal/sim's simulations both call (Rule 9).
func (s *Server) evaluatePolicy(doc *corepolicy.PolicyDocument, requestedModel, stickyKey string) corepolicy.Decision {
	return corepolicy.Evaluate(doc, requestedModel, stickyKey, s.rng)
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
