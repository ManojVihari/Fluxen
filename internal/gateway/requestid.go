package gateway

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
)

type requestIDKey struct{}

const RequestIDHeader = "X-Fluxen-Request-Id"

// RequestIDMiddleware generates a request ID for every proxied request
// (Part C.5/C.6), attaches it to the request context, and sets it on the
// response so the client can correlate its own logs with Fluxen's — and
// so the same ID becomes the requests.id primary key for this request's
// UsageRecord. It is a standard UUIDv4 (not just an opaque token) because
// requests.id is a Postgres uuid column (Part E.1).
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set(RequestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext returns the request ID set by RequestIDMiddleware,
// or "" if none is present (e.g. in a unit test that doesn't wire the
// middleware).
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// newRequestID generates a UUIDv4 using crypto/rand. Not worth a
// dependency for one function's worth of bit-twiddling (Rule 5: no
// dependency without a concrete need).
func newRequestID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failing is effectively unrecoverable, but a
		// collision-risking fallback is still better than crashing a
		// proxied request over an ID (Part B.2: never fail traffic for
		// an analytics/observability reason).
		buf = []byte{0xde, 0xad, 0xbe, 0xef, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	}
	buf[6] = (buf[6] & 0x0f) | 0x40 // version 4
	buf[8] = (buf[8] & 0x3f) | 0x80 // variant 10

	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}
