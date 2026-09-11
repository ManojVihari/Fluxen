package gateway

import (
	"encoding/json"
	"net/http"
)

// errorEnvelope mirrors OpenAI's error shape — the only client dialect
// Phase 1 speaks, so a Fluxen-originated error (invalid key, timeout, no
// credential configured) looks exactly like an error the client's own SDK
// already knows how to parse (Part C.5: "a response body shaped like the
// client's own protocol").
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// writeError writes a Fluxen-originated error response: the OpenAI-shaped
// JSON body, plus X-Fluxen-Error-Code and X-Fluxen-Request-Id headers
// (Part C.5) that let an operator distinguish "Fluxen rejected this" from
// "the provider rejected this" without parsing the body.
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	w.Header().Set("X-Fluxen-Error-Code", code)
	if id := RequestIDFromContext(r.Context()); id != "" {
		w.Header().Set(RequestIDHeader, id)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorEnvelope{Error: errorBody{
		Message: message,
		Type:    "fluxen_error",
		Code:    code,
	}})
}

// relayUpstreamError writes a provider's own error response verbatim —
// for Phase 1 (OpenAI in, OpenAI dialect out) this is correct by
// construction, since the client already expects an OpenAI-shaped error
// (Part D: "error normalization" in the trivial passthrough case).
func relayUpstreamError(w http.ResponseWriter, r *http.Request, status int, body []byte) {
	if id := RequestIDFromContext(r.Context()); id != "" {
		w.Header().Set(RequestIDHeader, id)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
