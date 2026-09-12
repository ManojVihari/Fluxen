package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"fluxen/pkg/providers"
	"fluxen/pkg/types"
)

// handleChatCompletions is the Phase 1 gateway pipeline (Part C.5, minus
// the policy/cache/route stages, which are no-ops until Phase 5):
//
//  1. authenticate  — done by AuthMiddleware before this handler runs
//  2. parse         — CanonicalRequest from the client body
//  3. invoke        — call the provider, streamed or not
//  4. account       — build the UsageRecord
//  5. emit          — push it onto the async ingest queue
//  6. respond       — relay the provider's response to the client
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
		cacheKey:       types.CacheKey(app.AppID, "", req),
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.Timeout)
	defer cancel()

	if req.Stream {
		s.handleChatStream(ctx, w, r, req, rb)
		return
	}
	s.handleChatUnary(ctx, w, r, req, rb)
}

func (s *Server) handleChatUnary(ctx context.Context, w http.ResponseWriter, r *http.Request, req *types.CanonicalRequest, rb *recordBuilder) {
	resp, err := s.Provider.Chat(ctx, req, s.Credential)
	if err != nil {
		s.handleProviderError(w, r, rb, err)
		return
	}

	rec := rb.finalize(s.Catalog, resp.Usage, resp.Model, "ok", http.StatusOK, "", "", nil)
	s.emit(rec)

	w.Header().Set(RequestIDHeader, rb.requestID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp.Raw)
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
