// Package types holds the domain types shared by every part of Fluxen that
// needs to talk about a request, its cost, or its usage — the gateway
// (production) and, from Phase 4 onward, the simulation engine (Rule 9:
// never duplicate pricing/policy logic between production and
// simulation — both must consume the same shapes).
package types

// Money is always integer micro-USD: 1 USD = 1_000_000. Never use float64
// for a cost figure anywhere in Fluxen — rounding must happen exactly
// once, at render time, never mid-calculation.
type Money int64

// CostStatus distinguishes "this cost is zero" from "this cost could not be
// determined" — collapsing the two would let an unknown model silently
// look like a free one (Part D.2 of the implementation specification).
type CostStatus string

const (
	// CostKnown means the cost was computed from a real pricing catalog
	// entry for the model that served the request.
	CostKnown CostStatus = "known"
	// CostUnknown means no catalog entry existed for the model. Cost is
	// reported as 0 but must never be treated as a real zero — it is
	// excluded from all savings math and efficiency scoring (Part D.2).
	CostUnknown CostStatus = "unknown"
	// CostLocal means the request was served by a local provider (Ollama)
	// that has no per-token bill. Cost is 0 by definition, not because it
	// is unknown (Part D.1). V1 never models GPU/electricity cost.
	CostLocal CostStatus = "local"
)
