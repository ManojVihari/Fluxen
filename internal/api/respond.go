package api

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

// maxJSONBodyBytes caps every control-API request body (bodySizeLimitMiddleware
// in server.go enforces it globally via http.MaxBytesReader). The control
// plane's payloads are all small forms/JSON documents — 1 MiB is
// generous headroom, not a tight fit — so this exists purely to stop an
// unbounded body from forcing the server to buffer arbitrary amounts of
// memory decoding it, not to constrain any real request.
const maxJSONBodyBytes = 1 << 20 // 1 MiB

// clientIP extracts the request's source IP for rate limiting. It reads
// only r.RemoteAddr, never X-Forwarded-For/X-Real-IP — those headers are
// attacker-controlled unless a reverse proxy is configured to strip and
// re-set them, and trusting them here without that guarantee would let a
// client bypass its own rate limit just by spoofing the header. A
// deployment behind a reverse proxy that terminates client connections
// itself (the standard Fluxen self-hosting shape once TLS is added) still
// rate-limits correctly, since RemoteAddr then reflects the proxy hop
// consistently rather than an unverified per-request claim.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return strings.TrimSpace(host)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}
