// Package providers defines the provider-neutral interface every upstream
// AI provider adapter implements (Part C.4). Phase 1 ships exactly one
// implementation, pkg/providers/openai; Gemini and Ollama are added in
// Phase 7.
package providers

import (
	"context"

	"fluxen/pkg/types"
)

// Credential is the minimum an adapter needs to authenticate against its
// provider. Phase 1 uses a single deployment-wide credential sourced from
// config (see internal/gateway) — the provider_credentials table (org-
// scoped, encrypted, UI-managed) is a Phase 7 concern; Credential's shape
// does not need to change when that arrives.
type Credential struct {
	APIKey  string
	BaseURL string // optional override, used for Ollama-style local endpoints and tests
}

// ModelInfo is one entry from a provider's model catalog (Models()).
type ModelInfo struct {
	ID string
}

// StreamReader yields the raw bytes of each streamed response chunk,
// exactly as received from the provider, so the gateway can relay them to
// the client without re-serializing (and therefore without any risk of
// dropping a field it doesn't model). Usage-extraction is provider-
// specific (Part C.6 — OpenAI reports usage on the final chunk via
// stream_options, Gemini and Ollama differently) so it lives behind this
// interface rather than in internal/gateway/stream.go, keeping Rule 7
// (provider-specific code stays inside pkg/providers) intact.
type StreamReader interface {
	// Next returns the next raw chunk to forward to the client. It
	// returns io.EOF when the upstream stream has ended normally.
	Next() ([]byte, error)
	// Usage returns the usage observed so far; call it after the stream
	// ends (or after a client abort) to build the final UsageRecord.
	Usage() types.ResponseUsage
	Close() error
}

// Provider is the interface every upstream AI provider adapter
// implements.
type Provider interface {
	Name() string
	Chat(ctx context.Context, req *types.CanonicalRequest, cred Credential) (*types.CanonicalResponse, error)
	ChatStream(ctx context.Context, req *types.CanonicalRequest, cred Credential) (StreamReader, error)
	Models(ctx context.Context, cred Credential) ([]ModelInfo, error)
	Health(ctx context.Context, cred Credential) error
}
