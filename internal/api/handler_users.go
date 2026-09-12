package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"fluxen/internal/auth"
	"fluxen/internal/store"
)

type userResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

func toUserResponse(u store.User) userResponse {
	return userResponse{ID: string(u.ID), Email: u.Email, Role: u.Role, CreatedAt: u.CreatedAt.Format("2006-01-02T15:04:05Z07:00")}
}

// handleListUsers is Settings > Users' owner/member list (Part I.6).
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)
	users, err := s.Users.ListByOrg(r.Context(), uc.OrgID)
	if err != nil {
		s.Logger.Error("api: failed to list users", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]userResponse, 0, len(users))
	for _, u := range users {
		out = append(out, toUserResponse(u))
	}
	writeJSON(w, http.StatusOK, out)
}

type inviteUserRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type inviteUserResponse struct {
	userResponse
	// TemporaryPassword is shown exactly once, the same convention the
	// API key creation flow already uses — Fluxen never stores or
	// re-displays a plaintext credential (Part G's security pass).
	TemporaryPassword string `json:"temporary_password"`
}

// handleInviteUser creates a new user directly with a generated
// temporary password (see auth.GenerateTemporaryPassword's doc comment
// for why this isn't a real emailed invite in V1).
func (s *Server) handleInviteUser(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)

	var req inviteUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	if req.Role != "owner" && req.Role != "member" {
		writeError(w, http.StatusBadRequest, "role must be owner or member")
		return
	}

	if _, err := s.Users.GetByEmail(r.Context(), req.Email); err == nil {
		writeError(w, http.StatusConflict, "a user with this email already exists")
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		s.Logger.Error("api: failed to check for an existing user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	tempPassword, err := auth.GenerateTemporaryPassword()
	if err != nil {
		s.Logger.Error("api: failed to generate temporary password", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hash, err := auth.HashPassword(tempPassword)
	if err != nil {
		s.Logger.Error("api: failed to hash temporary password", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	user, err := s.Users.Create(r.Context(), uc.OrgID, req.Email, hash, req.Role)
	if err != nil {
		s.Logger.Error("api: failed to create user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, inviteUserResponse{userResponse: toUserResponse(user), TemporaryPassword: tempPassword})
}
