package gateway

import (
	"time"

	corepolicy "fluxen/pkg/policy"
	"fluxen/pkg/pricing"
	"fluxen/pkg/types"
)

// recordBuilder accumulates everything the Account stage (Part C.5, step
// 10) needs across the pipeline, so the streaming and non-streaming paths
// can share one finalize step instead of duplicating UsageRecord
// construction. Fields below the workload line are set as the pipeline
// progresses (policy load, routing decision, cache lookup) rather than at
// construction, since Phase 5 introduced stages that run after parsing
// but before invoking the provider.
type recordBuilder struct {
	requestID string
	app       AppContext
	startedAt time.Time

	requestedModel string
	streamed       bool
	temperature    *float64
	maxTokens      *int
	workload       types.WorkloadFeatures
	cacheKey       []byte

	routeReason   string
	routeVariant  string
	policyVersion int
	policyDoc     *corepolicy.PolicyDocument
	// cacheStatus defaults to "disabled" (Part E.1) and is set to "miss"
	// by the handler right before a cache lookup when caching is enabled
	// for this application — finalize reads it as-is, since only the
	// handler knows whether a lookup was even attempted.
	cacheStatus string
}

// finalize builds the UsageRecord for one completed (or failed) request
// that actually called the provider (a cache hit uses finalizeCacheHit
// instead — it never touches pricing.Calculate, since nothing was
// bought). Every real pipeline exit path — success, upstream error,
// timeout, client abort, and now also blocked-by-policy — funnels
// through one of these two functions so cost/status accounting never
// drifts between the streaming and non-streaming code paths.
func (b *recordBuilder) finalize(catalog *pricing.Catalog, usage types.ResponseUsage, servedModel, status string, httpStatus int, errCode, errMsg string, ttft *int) types.UsageRecord {
	costInput, costOutput, costTotal, costStatus := pricing.Calculate(catalog, servedModel, usage)
	cacheStatus := b.cacheStatus
	if cacheStatus == "" {
		cacheStatus = "disabled"
	}
	return b.record(usage, servedModel, status, httpStatus, errCode, errMsg, ttft, costInput, costOutput, costTotal, costStatus, catalog.Version, cacheStatus, 0)
}

// finalizeBlocked builds the UsageRecord for a request policy blocked
// before ever reaching the provider (model restriction, rate limit, or
// budget) — Part E.1's status enum already includes "blocked" for
// exactly this, and cost is always zero since nothing was called.
func (b *recordBuilder) finalizeBlocked(httpStatus int, errCode, errMsg string) types.UsageRecord {
	return b.record(types.ResponseUsage{}, b.requestedModel, "blocked", httpStatus, errCode, errMsg, nil, 0, 0, 0, types.CostKnown, "", "bypass", 0)
}

// finalizeCacheHit builds the UsageRecord for a request served entirely
// from cache: the real cost is zero (nothing was bought), and
// CacheSavedCost records what the identical earlier request actually
// cost — Part E.1's cache_saved_micro is exactly this figure, the only
// place a cache hit's value shows up numerically.
func (b *recordBuilder) finalizeCacheHit(usage types.ResponseUsage, servedModel string, ttft *int, savedCostMicro int64) types.UsageRecord {
	return b.record(usage, servedModel, "ok", 200, "", "", ttft, 0, 0, 0, types.CostKnown, "", "hit", savedCostMicro)
}

func (b *recordBuilder) record(usage types.ResponseUsage, servedModel, status string, httpStatus int, errCode, errMsg string, ttft *int, costInput, costOutput, costTotal types.Money, costStatus types.CostStatus, pricingVersion, cacheStatus string, cacheSavedMicro int64) types.UsageRecord {
	durationMS := int(time.Since(b.startedAt) / time.Millisecond)

	return types.UsageRecord{
		ID:             b.requestID,
		OrgID:          b.app.OrgID,
		AppID:          b.app.AppID,
		APIKeyID:       b.app.APIKeyID,
		StartedAt:      b.startedAt,
		DurationMS:     durationMS,
		TTFTMS:         ttft,
		Endpoint:       "chat.completions",
		Protocol:       "openai",
		Streamed:       b.streamed,
		RequestedModel: b.requestedModel,
		Provider:       "openai",
		Model:          servedModel,
		RouteReason:    b.routeReason,
		RouteVariant:   b.routeVariant,
		PolicyVersion:  b.policyVersion,

		InputTokens:       usage.InputTokens,
		OutputTokens:      usage.OutputTokens,
		CachedInputTokens: usage.CachedInputTokens,
		TotalTokens:       usage.TotalTokens,
		UsageSource:       "provider",

		Cost:           costTotal,
		CostInput:      costInput,
		CostOutput:     costOutput,
		CostStatus:     costStatus,
		PricingVersion: pricingVersion,

		// CacheKey is computed and stored for every request regardless of
		// whether caching is enabled (Part G.3.2) — this is what let
		// Phase 4's exact-caching simulation, and what will let Phase 7's
		// Repeated Request detector, prove value before the feature
		// itself was ever turned on.
		CacheKey:       b.cacheKey,
		CacheStatus:    cacheStatus,
		CacheSavedCost: types.Money(cacheSavedMicro),

		Status:       status,
		HTTPStatus:   httpStatus,
		ErrorCode:    errCode,
		ErrorMessage: errMsg,

		Temperature:  b.temperature,
		MaxTokensReq: b.maxTokens,

		WorkloadFeatures: b.workload,
	}
}

// emit pushes a finalized record onto the ingest queue. It never returns
// an error the caller should act on — a full queue is a documented,
// metric-tracked tradeoff (Part B.2), not a request failure.
func (s *Server) emit(rec types.UsageRecord) {
	s.Queue.Enqueue(rec)
}
