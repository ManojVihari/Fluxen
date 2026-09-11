package types

import "time"

// UsageRecord is the single fact type Fluxen produces for every proxied
// request (Part C.3). It is built once per request by the gateway's
// Account stage, pushed onto the async ingest queue (never written
// synchronously — Part B.2's hot-path contract), and is the only input the
// requests table insert needs.
//
// OrgID, ID, and APIKeyID are bookkeeping fields the requests table
// requires (Part E.1) that aren't spelled out in the illustrative struct
// in Part C.3 — the request's own identity and the organization it
// belongs to, both already known by the time Account runs.
type UsageRecord struct {
	ID         string // the request's own ID (X-Fluxen-Request-Id)
	OrgID      OrgID
	AppID      AppID
	APIKeyID   APIKeyID
	StartedAt  time.Time
	DurationMS int
	TTFTMS     *int

	Endpoint string // "chat.completions" | "embeddings" | "generate"
	Protocol string // "openai" | "gemini" | "ollama"
	Streamed bool

	RequestedModel string
	Provider       string
	Model          string
	RouteReason    string // "direct" | "split" | "restriction" | "fallback"
	RouteVariant   string // "" | "A" | "B"
	PolicyVersion  int

	InputTokens       int
	OutputTokens      int
	CachedInputTokens int
	TotalTokens       int
	UsageSource       string // "provider" | "estimated"

	Cost           Money
	CostInput      Money
	CostOutput     Money
	CostStatus     CostStatus
	PricingVersion string

	CacheStatus    string // "hit" | "miss" | "bypass" | "disabled"
	CacheKey       []byte
	CacheSavedCost Money

	Status       string // "ok" | "provider_error" | "blocked" | "timeout" | "client_abort"
	HTTPStatus   int
	ErrorCode    string
	ErrorMessage string

	// Request parameters kept alongside the shape features below —
	// detectors read both together to build a workload profile (Part
	// G.3.1) without ever needing the request body itself.
	Temperature  *float64
	MaxTokensReq *int

	WorkloadFeatures
}
