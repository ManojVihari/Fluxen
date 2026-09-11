package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"fluxen/internal/auth"
)

type setupStatusResponse struct {
	Complete bool `json:"complete"`
}

// handleSetupStatus tells the dashboard whether setup has ever run, so its
// root page can route a visitor to the setup wizard vs. the login page
// (Part J's first-run flow: "setup wizard" only appears before an owner
// exists).
func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	any, err := s.Orgs.Any(r.Context())
	if err != nil {
		s.Logger.Error("api: failed to check setup status", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, setupStatusResponse{Complete: any})
}

type setupRequest struct {
	OrgName  string `json:"org_name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type setupResponse struct {
	OrgID string `json:"org_id"`
	Email string `json:"email"`
}

// handleSetup creates the organization and its first (owner) user. It is
// only callable once per deployment — a second call, once any
// organization exists, is rejected with 409 (Part L Phase 1: "POST
// /api/v1/setup (create org + owner, once)").
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var req setupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.OrgName = strings.TrimSpace(req.OrgName)
	req.Email = strings.TrimSpace(req.Email)
	if req.OrgName == "" || req.Email == "" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "org_name and email are required; password must be at least 8 characters")
		return
	}

	alreadySetUp, err := s.Orgs.Any(r.Context())
	if err != nil {
		s.Logger.Error("api: failed to check setup status", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if alreadySetUp {
		writeError(w, http.StatusConflict, "Fluxen has already been set up")
		return
	}

	org, err := s.Orgs.Create(r.Context(), req.OrgName)
	if err != nil {
		s.Logger.Error("api: failed to create organization", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.Logger.Error("api: failed to hash password", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	user, err := s.Users.Create(r.Context(), org.ID, req.Email, hash, "owner")
	if err != nil {
		s.Logger.Error("api: failed to create owner user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	session, err := s.Sessions.Create(r.Context(), string(user.ID), string(org.ID))
	if err != nil {
		s.Logger.Error("api: failed to create session", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	setSessionCookie(w, session.Token)

	writeJSON(w, http.StatusCreated, setupResponse{OrgID: string(org.ID), Email: user.Email})
}
