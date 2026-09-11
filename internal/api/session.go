package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"fluxen/internal/auth"
	"fluxen/pkg/types"
)

type userContextKey struct{}

// UserContext is what requireAuth attaches to an authenticated request.
type UserContext struct {
	UserID types.UserID
	OrgID  types.OrgID
}

func userFromRequest(r *http.Request) (UserContext, bool) {
	uc, ok := r.Context().Value(userContextKey{}).(UserContext)
	return uc, ok
}

// requireAuth resolves the session cookie and rejects the request with 401
// if it's missing, expired, or invalid.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(auth.SessionCookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "not authenticated")
			return
		}

		session, err := s.Sessions.Get(r.Context(), cookie.Value)
		if err != nil {
			if !errors.Is(err, auth.ErrSessionNotFound) {
				s.Logger.Error("api: failed to read session", "error", err)
			}
			writeError(w, http.StatusUnauthorized, "not authenticated")
			return
		}

		uc := UserContext{UserID: session.UserID, OrgID: session.OrgID}
		ctx := context.WithValue(r.Context(), userContextKey{}, uc)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// setSessionCookie writes the session cookie for a newly-created session.
func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Secure is deliberately left false so local/dev/default-compose
		// deployments over plain HTTP work out of the box. Requiring TLS
		// termination in front of Fluxen for a secure cookie is a Phase 9
		// security-hardening concern (Part L Phase 9), not a Phase 1 one.
		MaxAge: int(auth.SessionTTL.Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}
