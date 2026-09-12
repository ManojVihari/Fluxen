package types

import (
	"crypto/sha256"
	"encoding/json"
)

// cacheKeyFields is the exact, fixed field set Part G.3.2 defines for the
// cache key — deliberately narrower than CanonicalRequest.JSON()'s full
// wire reconstruction: no Stream, no Extra. Two requests that differ only
// in whether they stream, or in a parameter Fluxen doesn't model, must
// still be recognized as the same cacheable request. Field order here is
// fixed by the struct definition, not map iteration, so the same request
// always canonicalizes to the same bytes.
type cacheKeyFields struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages,omitempty"`
	Tools          []Tool          `json:"tools,omitempty"`
	ToolChoice     json.RawMessage `json:"tool_choice,omitempty"`
	ResponseFormat json.RawMessage `json:"response_format,omitempty"`
	Temperature    *float64        `json:"temperature,omitempty"`
	TopP           *float64        `json:"top_p,omitempty"`
	MaxTokens      *int            `json:"max_tokens,omitempty"`
	Stop           []string        `json:"stop,omitempty"`
	Seed           *int            `json:"seed,omitempty"`
	N              *int            `json:"n,omitempty"`
}

// CacheKey computes the exact-match cache key (Part G.3.2):
// sha256(app_id ‖ namespace ‖ canonical_json({model, messages, tools,
// tool_choice, response_format, temperature, top_p, max_tokens, stop,
// seed, n})). It is computed for every request regardless of whether
// caching is enabled — this is what lets the Repeated Request detector
// (Phase 7) and the exact-caching simulation (Phase 4) prove value
// before the feature itself is ever turned on.
//
// namespace is reserved for future per-scope cache partitioning — Part
// G.3.2 names it in the formula but nothing else in the V1 specification
// defines what it should hold, so every V1 caller passes "".
//
// This is an exact-match key only (Part G.3.2's "exact caching only"):
// two logically-identical requests whose message JSON differs only in
// insignificant whitespace hash differently. Real SDKs serialize
// consistently, so this doesn't matter in practice, and semantic/fuzzy
// caching is explicitly out of scope for V1 (Part A.2).
func CacheKey(appID AppID, namespace string, req *CanonicalRequest) []byte {
	fields := cacheKeyFields{
		Model: req.Model, Messages: req.Messages, Tools: req.Tools,
		ToolChoice: req.ToolChoice, ResponseFormat: req.ResponseFormat,
		Temperature: req.Temperature, TopP: req.TopP, MaxTokens: req.MaxTokens,
		Stop: req.Stop, Seed: req.Seed, N: req.N,
	}
	// Marshaling a struct built entirely from already-valid
	// json.RawMessage/primitive fields cannot fail.
	canonicalJSON, _ := json.Marshal(fields)

	h := sha256.New()
	h.Write([]byte(appID))
	h.Write([]byte{0}) // separator: avoids "app1"+"2ns" colliding with "app12"+"ns"
	h.Write([]byte(namespace))
	h.Write([]byte{0})
	h.Write(canonicalJSON)
	return h.Sum(nil)
}
