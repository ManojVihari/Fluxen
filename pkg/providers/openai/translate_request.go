package openai

import (
	"encoding/json"

	"fluxen/pkg/types"
)

// buildRequestBody reconstructs the OpenAI wire request from a
// CanonicalRequest. Because CanonicalRequest's shape already is the
// OpenAI dialect (Part C.3), this is mostly CanonicalRequest.JSON() — the
// one OpenAI-specific step is ensuring a streaming request always asks for
// usage on its final chunk, since Fluxen cannot account for a streamed
// request otherwise (Part C.6).
func buildRequestBody(req *types.CanonicalRequest) ([]byte, error) {
	if req.Stream {
		ensureStreamUsage(req)
	}
	return req.JSON()
}

// ensureStreamUsage injects {"include_usage": true} into stream_options
// unless the client already set one explicitly. If the client explicitly
// disabled it, that quiet choice is honored — the gateway falls back to
// usage_source="estimated" in that case (Part C.6) rather than overriding
// what the caller asked for.
func ensureStreamUsage(req *types.CanonicalRequest) {
	existing, ok := req.Extra["stream_options"]
	if !ok {
		req.Extra["stream_options"] = json.RawMessage(`{"include_usage":true}`)
		return
	}

	var opts map[string]json.RawMessage
	if err := json.Unmarshal(existing, &opts); err != nil {
		// Malformed stream_options from the client — leave it exactly as
		// given rather than guessing; OpenAI will reject it and the error
		// relays back to the client untouched.
		return
	}
	if _, set := opts["include_usage"]; set {
		return
	}
	opts["include_usage"] = json.RawMessage("true")
	if b, err := json.Marshal(opts); err == nil {
		req.Extra["stream_options"] = b
	}
}

// requestWantsUsage reports whether the outgoing request will cause
// OpenAI to include a usage object on the final streamed chunk, so the
// gateway knows whether to expect provider-reported usage or fall back to
// UsageSource="estimated".
func requestWantsUsage(req *types.CanonicalRequest) bool {
	existing, ok := req.Extra["stream_options"]
	if !ok {
		return true // ensureStreamUsage will have set it
	}
	var opts map[string]json.RawMessage
	if err := json.Unmarshal(existing, &opts); err != nil {
		return false
	}
	v, ok := opts["include_usage"]
	if !ok {
		return true
	}
	return string(v) == "true"
}
