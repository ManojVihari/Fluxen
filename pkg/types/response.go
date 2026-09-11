package types

import "encoding/json"

// ResponseUsage is the token accounting extracted from a provider
// response, in the provider's own reported units. UsageSource on
// UsageRecord records whether these numbers came from the provider or
// were estimated (Part C.3) — Phase 1 only ever uses "provider", since
// OpenAI always reports usage.
type ResponseUsage struct {
	InputTokens       int
	OutputTokens      int
	CachedInputTokens int
	ReasoningTokens   int
	TotalTokens       int
}

// CanonicalResponse is what a Provider.Chat call returns for a
// non-streaming request. Raw holds the exact response body bytes as
// received from the provider — for Phase 1 (OpenAI in, OpenAI-dialect
// out) the gateway relays Raw directly to the client rather than
// re-serializing from the typed fields, so no unknown response field is
// ever dropped just because Fluxen didn't model it. The typed fields exist
// for accounting (pricing, workload features, detectors), not for
// reconstructing the client-facing body.
type CanonicalResponse struct {
	ID    string
	Model string
	Usage ResponseUsage
	Raw   json.RawMessage
}
