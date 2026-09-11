package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"fluxen/internal/store"
	"fluxen/pkg/types"
)

type createKeyRequest struct {
	Name string `json:"name"`
}

type createKeyResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	Key       string    `json:"key"` // shown exactly once — never returned again
	CreatedAt time.Time `json:"created_at"`
}

// handleCreateKey issues a new API key for an application the caller's
// org owns. The raw key is returned exactly once (Part L Phase 1: "API
// key issuance screen showing the raw key exactly once").
func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)
	appID := types.AppID(chi.URLParam(r, "appID"))

	// Confirm the application belongs to the caller's org before issuing
	// a key for it — Get is org-scoped (Part E: "one org can never read
	// another's application").
	if _, err := s.Apps.Get(r.Context(), uc.OrgID, appID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "application not found")
			return
		}
		s.Logger.Error("api: failed to look up application", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var req createKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = "default"
	}

	raw, key, err := s.Keys.Create(r.Context(), appID, req.Name)
	if err != nil {
		s.Logger.Error("api: failed to create api key", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, createKeyResponse{
		ID: string(key.ID), Name: key.Name, Prefix: key.Prefix, Key: raw, CreatedAt: key.CreatedAt,
	})
}

// handleRevokeKey revokes a key. If the API and gateway share a process
// (Phase 1's combined cmd/fluxen binary), the revocation also invalidates
// the gateway's in-process resolver cache entry immediately.
func (s *Server) handleRevokeKey(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)
	keyID := types.APIKeyID(chi.URLParam(r, "keyID"))

	prefix, err := s.Keys.Revoke(r.Context(), uc.OrgID, keyID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "api key not found")
			return
		}
		s.Logger.Error("api: failed to revoke api key", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if s.KeyResolver != nil {
		s.KeyResolver.Invalidate(prefix)
	}

	w.WriteHeader(http.StatusNoContent)
}
