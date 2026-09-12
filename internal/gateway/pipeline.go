package gateway

import (
	"time"

	"fluxen/pkg/pricing"
	"fluxen/pkg/types"
)

// recordBuilder accumulates everything the Account stage (Part C.5, step
// 10) needs across the pipeline, so the streaming and non-streaming paths
// can share one finalize step instead of duplicating UsageRecord
// construction.
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
}

// finalize builds the UsageRecord for one completed (or failed) request.
// Every pipeline exit path — success, upstream error, timeout, client
// abort — funnels through this one function so cost/status accounting
// never drifts between the streaming and non-streaming code paths.
func (b *recordBuilder) finalize(catalog *pricing.Catalog, usage types.ResponseUsage, servedModel, status string, httpStatus int, errCode, errMsg string, ttft *int) types.UsageRecord {
	costInput, costOutput, costTotal, costStatus := pricing.Calculate(catalog, servedModel, usage)

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
		RouteReason:    "direct", // no routing until Phase 5
		PolicyVersion:  0,        // no policy engine until Phase 5

		InputTokens:       usage.InputTokens,
		OutputTokens:      usage.OutputTokens,
		CachedInputTokens: usage.CachedInputTokens,
		TotalTokens:       usage.TotalTokens,
		UsageSource:       "provider",

		Cost:           costTotal,
		CostInput:      costInput,
		CostOutput:     costOutput,
		CostStatus:     costStatus,
		PricingVersion: catalog.Version,

		// CacheKey is computed and stored for every request regardless of
		// whether caching is enforced (Part G.3.2) — this is what lets
		// Phase 4's exact-caching simulation, and Phase 7's Repeated
		// Request detector, prove value before the feature itself exists.
		CacheStatus: "disabled", // no cache enforcement until Phase 5
		CacheKey:    b.cacheKey,

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
