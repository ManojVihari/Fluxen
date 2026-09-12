package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"fluxen/internal/cache"
	corepolicy "fluxen/pkg/policy"
	"fluxen/pkg/providers"
	"fluxen/pkg/types"
)

// handleChatCompletions is the gateway pipeline (Part C.5):
//
//  1. authenticate      — done by AuthMiddleware before this handler runs
//  2. parse             — CanonicalRequest from the client body
//  3. policy            — load the application's policy snapshot
//  4. restrict + route  — pkg/policy.Evaluate: block or reroute the model
//  5. rate limit/budget — internal/guard, fail-open (Rule 20)
//  6. cache             — internal/cache lookup; a hit skips straight to
//     respond
//  7. invoke            — call the provider, streamed or not
//  8. account           — build the UsageRecord, record budget spend,
//     populate the cache on a miss
//  9. emit              — push the UsageRecord onto the async ingest queue
//  10. respond          — relay the response to the client
//
// Every stage from 3 onward is nil-safe (Server.Policy/RateLimiter/
// BudgetGuard/Cache may be nil): a Server without them behaves exactly
// like Phase 1's pipeline — every request direct, uncached, unmetered,
// unrestricted.
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	app, ok := AppContextFromRequest(r)
	if !ok {
		// AuthMiddleware always sets this before routing here; this is a
		// defensive fallback, not a reachable path in production.
		writeError(w, r, http.StatusUnauthorized, "invalid_api_key", "Missing application context.")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, MaxRequestBodyBytes+1))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "Failed to read request body.")
		return
	}
	if len(body) > MaxRequestBodyBytes {
		writeError(w, r, http.StatusRequestEntityTooLarge, "request_too_large", "Request body exceeds the maximum allowed size.")
		return
	}

	req, err := types.ParseCanonicalRequest(body)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if s.Credential.APIKey == "" {
		writeError(w, r, http.StatusServiceUnavailable, "no_provider_credential", "No OpenAI API key is configured for this Fluxen deployment.")
		return
	}

	rb := &recordBuilder{
		requestID:      RequestIDFromContext(r.Context()),
		app:            app,
		startedAt:      startedAt,
		requestedModel: req.Model,
		streamed:       req.Stream,
		temperature:    req.Temperature,
		maxTokens:      req.MaxTokens,
		workload:       types.ExtractWorkloadFeatures(req),
		routeReason:    "direct",
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.Timeout)
	defer cancel()

	doc, policyVersion := s.loadPolicy(ctx, app.AppID)
	rb.policyDoc = doc
	rb.policyVersion = policyVersion

	// Sticky routing keys by the calling API key — the closest V1 has to
	// a "session" for gateway traffic (no per-conversation identity is
	// ever sent by a stock OpenAI client, Part F Rule 20).
	decision := s.evaluatePolicy(doc, req.Model, string(app.APIKeyID))
	rb.routeReason = decision.RouteReason
	rb.routeVariant = decision.RouteVariant
	if decision.Blocked {
		s.emit(rb.finalizeBlocked(http.StatusForbidden, decision.BlockCode, "Model not allowed by this application's policy."))
		writeError(w, r, http.StatusForbidden, decision.BlockCode, "This application's policy does not allow this model.")
		return
	}
	req.Model = decision.Model // actually route the call to the decided model

	if s.RateLimiter != nil && !s.RateLimiter.Allow(ctx, app.AppID, doc.RateLimit) {
		s.emit(rb.finalizeBlocked(http.StatusTooManyRequests, "rate_limited", "Rate limit exceeded."))
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", "This application has exceeded its configured rate limit.")
		return
	}

	if s.BudgetGuard != nil && s.BudgetGuard.Blocked(ctx, app.AppID, doc.Budget) {
		s.emit(rb.finalizeBlocked(http.StatusForbidden, "budget_exceeded", "Budget exceeded."))
		writeError(w, r, http.StatusForbidden, "budget_exceeded", "This application has exceeded its configured budget.")
		return
	}

	// Computed after routing so the key reflects the model that will
	// actually serve the request — two routing outcomes for the same
	// client request must not share a cache entry (Part G.3.2's formula
	// includes model precisely so this can't happen).
	rb.cacheKey = types.CacheKey(app.AppID, "", req)

	if s.Cache != nil && doc.Caching != nil && doc.Caching.Enabled {
		rb.cacheStatus = "miss"
		if entry, hit := s.Cache.Get(ctx, app.AppID, rb.cacheKey); hit {
			s.serveFromCache(w, r, rb, entry, startedAt)
			return
		}
	}

	if req.Stream {
		s.handleChatStream(ctx, w, r, req, rb)
		return
	}
	s.handleChatUnary(ctx, w, r, req, rb)
}

// loadPolicy fetches the application's current policy and version,
// failing open to the empty (no-op) document at version 0 on any error —
// a control-plane hiccup must never take down the gateway's own hot path.
func (s *Server) loadPolicy(ctx context.Context, appID types.AppID) (*corepolicy.PolicyDocument, int) {
	if s.Policy == nil {
		return &corepolicy.PolicyDocument{}, 0
	}
	doc, version, err := s.Policy.Get(ctx, appID)
	if err != nil {
		s.Logger.Warn("gateway: failed to load policy, proceeding with no policy", "app_id", appID, "error", err)
		return &corepolicy.PolicyDocument{}, 0
	}
	return &doc, version
}

func (s *Server) handleChatUnary(ctx context.Context, w http.ResponseWriter, r *http.Request, req *types.CanonicalRequest, rb *recordBuilder) {
	resp, err := s.Provider.Chat(ctx, req, s.Credential)
	if err != nil {
		s.handleProviderError(w, r, rb, err)
		return
	}

	rec := rb.finalize(s.Catalog, resp.Usage, resp.Model, "ok", http.StatusOK, "", "", nil)
	s.emit(rec)
	s.afterSuccess(ctx, rb, rec, resp.Raw, nil)

	w.Header().Set(RequestIDHeader, rb.requestID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp.Raw)
}

// afterSuccess runs the two post-success side effects that never affect
// the response already sent to the client: recording budget spend and
// populating the cache for a future identical request. Both are
// best-effort — internal/guard and internal/cache already log their own
// failures and never propagate them here.
func (s *Server) afterSuccess(ctx context.Context, rb *recordBuilder, rec types.UsageRecord, body []byte, chunks [][]byte) {
	if s.BudgetGuard != nil && rb.policyDoc.Budget != nil {
		s.BudgetGuard.RecordSpend(ctx, rb.app.AppID, rb.policyDoc.Budget.Period, int64(rec.Cost))
	}
	if s.Cache != nil && rb.policyDoc.Caching != nil && rb.policyDoc.Caching.Enabled && len(rb.cacheKey) > 0 {
		ttl := time.Duration(rb.policyDoc.Caching.TTLSeconds) * time.Second
		s.Cache.Set(ctx, rb.app.AppID, rb.cacheKey, cache.Entry{
			Streamed: rb.streamed, Body: body, Chunks: chunks,
			Model: rec.Model, InputTokens: rec.InputTokens, OutputTokens: rec.OutputTokens, TotalTokens: rec.TotalTokens,
			CostMicro: int64(rec.Cost),
		}, ttl)
	}
}

// serveFromCache replays a hit verbatim (Part L Phase 5: "SSE replay for
// cached streaming responses"). Streaming chunks are replayed in the
// exact order and bytes they were originally written — no re-parsing,
// no re-synthesizing — so a cached streamed response is indistinguishable
// from the original to the client.
func (s *Server) serveFromCache(w http.ResponseWriter, r *http.Request, rb *recordBuilder, entry cache.Entry, startedAt time.Time) {
	usage := types.ResponseUsage{InputTokens: entry.InputTokens, OutputTokens: entry.OutputTokens, TotalTokens: entry.TotalTokens}

	if entry.Streamed {
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, r, http.StatusInternalServerError, "streaming_unsupported", "This server does not support streaming responses.")
			return
		}
		w.Header().Set(RequestIDHeader, rb.requestID)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		var ttft *int
		for i, chunk := range entry.Chunks {
			if _, err := w.Write(chunk); err != nil {
				break // client went away; the hit is still free either way
			}
			flusher.Flush()
			if i == 0 {
				ms := int(time.Since(startedAt) / time.Millisecond)
				ttft = &ms
			}
		}
		s.emit(rb.finalizeCacheHit(usage, entry.Model, ttft, entry.CostMicro))
		return
	}

	s.emit(rb.finalizeCacheHit(usage, entry.Model, nil, entry.CostMicro))

	w.Header().Set(RequestIDHeader, rb.requestID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(entry.Body)
}

// handleProviderError covers every way a provider call can fail:
// upstream returned a non-2xx (relayed verbatim, Part D), a timeout
// (504 upstream_timeout), or a transport-level failure
// (502 provider_unavailable). Every branch still emits a UsageRecord —
// Fluxen accounts for failed requests, not just successful ones.
func (s *Server) handleProviderError(w http.ResponseWriter, r *http.Request, rb *recordBuilder, err error) {
	var upstreamErr *providers.UpstreamError
	if errors.As(err, &upstreamErr) {
		rec := rb.finalize(s.Catalog, types.ResponseUsage{}, rb.requestedModel, "provider_error", upstreamErr.StatusCode, "provider_error", err.Error(), nil)
		s.emit(rec)
		relayUpstreamError(w, r, upstreamErr.StatusCode, upstreamErr.Body)
		return
	}

	if errors.Is(err, context.DeadlineExceeded) {
		rec := rb.finalize(s.Catalog, types.ResponseUsage{}, rb.requestedModel, "timeout", http.StatusGatewayTimeout, "upstream_timeout", err.Error(), nil)
		s.emit(rec)
		writeError(w, r, http.StatusGatewayTimeout, "upstream_timeout", "The upstream provider did not respond in time.")
		return
	}

	rec := rb.finalize(s.Catalog, types.ResponseUsage{}, rb.requestedModel, "provider_error", http.StatusBadGateway, "provider_unavailable", err.Error(), nil)
	s.emit(rec)
	writeError(w, r, http.StatusBadGateway, "provider_unavailable", "Failed to reach the upstream provider.")
}
