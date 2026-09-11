package gateway

import (
	"context"
	"net/http"
	"strings"

	"fluxen/internal/auth"
)

type ctxKey int

const appContextKey ctxKey = iota

// AppContextFromRequest returns the AppContext attached by
// AuthMiddleware, and whether one was present.
func AppContextFromRequest(r *http.Request) (AppContext, bool) {
	ac, ok := r.Context().Value(appContextKey).(AppContext)
	return ac, ok
}

// AuthMiddleware resolves the Authorization: Bearer fx_... header to an
// AppContext (Part C.5, pipeline stage 1: "Authenticate"). A missing,
// malformed, unknown, or revoked key all produce the same 401
// invalid_api_key response — Fluxen doesn't leak which case it was.
func AuthMiddleware(resolver *auth.Resolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := bearerToken(r)
			if !ok {
				writeError(w, r, http.StatusUnauthorized, "invalid_api_key", "Missing or malformed Authorization header.")
				return
			}

			rec, err := resolver.Resolve(r.Context(), raw)
			if err != nil {
				writeError(w, r, http.StatusUnauthorized, "invalid_api_key", "Invalid or revoked API key.")
				return
			}

			ac := AppContext{AppID: rec.AppID, OrgID: rec.OrgID, APIKeyID: rec.ID}
			ctx := context.WithValue(r.Context(), appContextKey, ac)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, prefix))
	if token == "" {
		return "", false
	}
	return token, true
}
