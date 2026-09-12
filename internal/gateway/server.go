package gateway

import (
	"context"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"fluxen/internal/auth"
	"fluxen/internal/cache"
	"fluxen/internal/credentials"
	"fluxen/internal/guard"
	"fluxen/internal/ingest"
	"fluxen/internal/policy"
	"fluxen/internal/retention"
	corepolicy "fluxen/pkg/policy"
	"fluxen/pkg/pricing"
	"fluxen/pkg/providers"
	"fluxen/pkg/types"
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
	Resolver *auth.Resolver
	// Provider/Credential are the OpenAI binding — the only provider
	// Phase 1 through Phase 6 ever had, kept as named fields (rather than
	// folded into Providers/Credentials below) so every existing test
	// construction keeps working unchanged.
	Provider   providers.Provider
	Credential providers.Credential
	// Providers/Credentials are the Phase 7 addition: Gemini and/or
	// Ollama bindings, keyed by provider name ("gemini", "ollama"). Both
	// are nil-safe — a deployment (or test) that never sets them behaves
	// exactly like Phase 1-6's OpenAI-only gateway (resolveProvider always
	// falls back to Provider/Credential).
	Providers   map[string]providers.Provider
	Credentials map[string]providers.Credential
	// CredentialSnapshot is the Phase 7 dynamic source: org-managed,
	// encrypted provider_credentials (Part E.1), consulted before the
	// static Credentials map above. Nil-safe — a deployment that never
	// sets it (or a provider that has no row yet) behaves exactly like
	// the static-only Credentials map.
	CredentialSnapshot *credentials.Snapshot
	// RetentionSettings gates Part G.6's opt-in request/response body
	// capture (Settings > Retention, off by default) — nil-safe, same as
	// every other optional dependency here: a Server without it never
	// captures a body, exactly the off-by-default behavior anyway.
	RetentionSettings *retention.Snapshot
	Catalog           *pricing.Catalog
	Queue             *ingest.Queue
	Policy            *policy.Snapshot
	RateLimiter       *guard.RateLimiter
	BudgetGuard       *guard.BudgetGuard
	Cache             *cache.Store
	Logger            *slog.Logger
	Timeout           time.Duration

	rng *rand.Rand
}

// resolveProvider decides which of the three providers actually serves a
// (possibly rerouted) model, and returns that provider's binding. The
// pricing catalog's own provider field is the source of truth for
// OpenAI/Gemini models (Part A.3: "routing to OpenAI/Gemini/Ollama" from
// one ingress); a model absent from the catalog is, by construction,
// never one Fluxen prices (Part D.1) — if Ollama actually has a
// credential configured (hasCredentialFor, checked against both the
// static env-var map and the org-managed snapshot — never merely
// whether a stateless client instance exists, since that's constructed
// unconditionally at boot), an uncataloged model is assumed to be a
// locally self-hosted one and routed there, since Ollama's model set can
// never be statically enumerated the way OpenAI/Gemini's can.
//
// This is a deliberate, documented interpretation where the specification
// doesn't pin an exact resolution rule: it means a genuinely mistyped or
// removed OpenAI/Gemini model name will silently route to Ollama instead
// of surfacing as "unknown model" whenever Ollama is configured — flagged
// here for visibility rather than decided silently.
func (s *Server) resolveProvider(ctx context.Context, orgID types.OrgID, model string) (providers.Provider, providers.Credential, string) {
	name := "openai"
	if s.Catalog != nil {
		if price, ok := s.Catalog.Lookup(model); ok && price.Provider != "" {
			name = price.Provider
		} else if s.hasCredentialFor(ctx, orgID, "ollama") {
			name = "ollama"
		}
	}

	client := s.Provider
	if name != "openai" {
		client = s.Providers[name]
	}

	// The org-managed, encrypted credential (Part E.1) takes priority
	// over the static env-var one whenever a row actually exists for
	// this provider — a deployment can migrate one provider at a time
	// without breaking the others.
	if s.CredentialSnapshot != nil {
		cred, found, err := s.CredentialSnapshot.Get(ctx, orgID, name)
		if err != nil {
			s.Logger.Warn("gateway: failed to resolve credential snapshot, falling back to static config", "provider", name, "error", err)
		} else if found {
			return client, cred, name
		}
	}

	if name == "openai" {
		return s.Provider, s.Credential, "openai"
	}
	return client, s.Credentials[name], name
}

// hasCredentialFor reports whether provider has a usable credential from
// either source — the static env-var map or the org-managed snapshot —
// without deciding which one resolveProvider will ultimately use. It
// exists so an uncataloged model's fallback-to-Ollama decision (Part
// A.3) is driven by "is Ollama actually configured for this deployment,"
// not merely "does a stateless Ollama HTTP client instance exist" (that
// client is always constructed at boot regardless of credentials, so it
// alone was never a safe signal).
func (s *Server) hasCredentialFor(ctx context.Context, orgID types.OrgID, provider string) bool {
	if _, ok := s.Credentials[provider]; ok {
		return true
	}
	if s.CredentialSnapshot != nil {
		if _, found, err := s.CredentialSnapshot.Get(ctx, orgID, provider); err == nil && found {
			return true
		}
	}
	return false
}

// credentialConfigured reports whether a resolved binding is usable.
// Ollama is commonly run with no authentication at all (Part D: a stock
// local install), so an empty APIKey is valid for it as long as a
// binding was configured (a non-nil provider) — OpenAI and Gemini always
// require a real key.
func credentialConfigured(providerName string, provider providers.Provider, cred providers.Credential) bool {
	if provider == nil {
		return false
	}
	if providerName == "ollama" {
		return true
	}
	return cred.APIKey != ""
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
