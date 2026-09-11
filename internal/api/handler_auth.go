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

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Email = strings.TrimSpace(req.Email)

	user, err := s.Users.GetByEmail(r.Context(), req.Email)
	if err != nil {
		// Same response whether the email doesn't exist or the password
		// is wrong — don't leak which one it was.
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
	setSessionCookie(w, session.Token)

	writeJSON(w, http.StatusOK, loginResponse{Email: user.Email})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
		if err := s.Sessions.Delete(r.Context(), cookie.Value); err != nil {
			s.Logger.Error("api: failed to delete session", "error", err)
		}
	}
	clearSessionCookie(w)
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
