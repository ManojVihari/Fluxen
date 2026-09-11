// Package gateway is the hot path (Part C.5): the OpenAI-compatible
// ingress applications point at Fluxen. Phase 1 implements exactly the
// pipeline steps needed to prove Application → Fluxen Gateway → Provider →
// Response: authenticate, parse, invoke, account, emit, respond. Policy,
// cache, and routing are no-op stages, wired for real starting in Phase 5
// — see pipeline.go.
package gateway

import "fluxen/pkg/types"

// AppContext is what key resolution attaches to a request: the identity
// the rest of the pipeline (and the eventual UsageRecord) needs.
type AppContext struct {
	AppID    types.AppID
	OrgID    types.OrgID
	APIKeyID types.APIKeyID
}
