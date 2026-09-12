package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"fluxen/internal/auth"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	Email string `json:"email"`
}

// dummyPasswordHash is a real bcrypt hash of a fixed, unrelated string —
// never the hash of any actual account. handleLogin runs a bcrypt compare
// against it whenever the email doesn't exist, so a failed login takes
// roughly the same time either way. Without this, an unknown email
// returns in the time of one DB lookup while a known one also pays
// bcrypt's ~100ms, letting an attacker enumerate valid accounts purely by
// timing the response.
const dummyPasswordHash = "$2a$10$cmVGK0lkMJzXnwCGcO7ArO7L5DAUFua84Uk7v6S8KqX8.C/8NzdVW"

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.LoginLimiter != nil && !s.LoginLimiter.Allow(r.Context(), clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "too many login attempts — try again in a few minutes")
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Email = strings.TrimSpace(req.Email)

	user, err := s.Users.GetByEmail(r.Context(), req.Email)
	if err != nil {
		// Same response whether the email doesn't exist or the password
		// is wrong — don't leak which one it was — and the same bcrypt
		// cost either way, so timing doesn't leak it instead. See
		// dummyPasswordHash's doc comment.
		auth.VerifyPassword(dummyPasswordHash, req.Password)
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if !auth.VerifyPassword(user.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	session, err := s.Sessions.Create(r.Context(), string(user.ID), string(user.OrgID))
	if err != nil {
		s.Logger.Error("api: failed to create session", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.setSessionCookie(w, session.Token)

	writeJSON(w, http.StatusOK, loginResponse{Email: user.Email})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
		if err := s.Sessions.Delete(r.Context(), cookie.Value); err != nil {
			s.Logger.Error("api: failed to delete session", "error", err)
		}
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

type sessionResponse struct {
	UserID string `json:"user_id"`
	OrgID  string `json:"org_id"`
	Email  string `json:"email"`
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(auth.SessionCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	session, err := s.Sessions.Get(r.Context(), cookie.Value)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	user, err := s.Users.Get(r.Context(), session.UserID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	writeJSON(w, http.StatusOK, sessionResponse{
		UserID: string(user.ID),
		OrgID:  string(user.OrgID),
		Email:  user.Email,
	})
}
