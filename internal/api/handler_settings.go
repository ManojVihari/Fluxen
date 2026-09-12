package api

import (
	"encoding/json"
	"net/http"

	"fluxen/internal/store"
)

// settingsResponse is Settings > Retention's three knobs (Part E.2/I.6).
// Users/Pricing settings are read through their own existing endpoints
// (users list, the embedded catalog) rather than folded in here — this
// resource is specifically the org-level values PATCH can actually
// change.
type settingsResponse struct {
	RequestsRetentionDays int  `json:"requests_retention_days"`
	BodyRetentionDays     int  `json:"body_retention_days"`
	BodyCaptureEnabled    bool `json:"body_capture_enabled"`
}

// handleGetSettings returns the org's current retention knobs.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)
	settings, err := s.Orgs.GetRetentionSettings(r.Context(), uc.OrgID)
	if err != nil {
		s.Logger.Error("api: failed to load settings", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, settingsResponse{
		RequestsRetentionDays: settings.RequestsRetentionDays,
		BodyRetentionDays:     settings.BodyRetentionDays,
		BodyCaptureEnabled:    settings.BodyCaptureEnabled,
	})
}

// handlePatchSettings updates the org's retention knobs. Both day counts
// must be positive — a zero or negative retention window is never a
// valid "keep nothing"/"keep forever" spelling in this API (there's no
// such shorthand; an operator who wants near-immediate deletion sets a
// small positive number instead).
func (s *Server) handlePatchSettings(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)

	var req settingsResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RequestsRetentionDays <= 0 || req.BodyRetentionDays <= 0 {
		writeError(w, http.StatusBadRequest, "retention windows must be a positive number of days")
		return
	}

	settings := store.RetentionSettings{
		RequestsRetentionDays: req.RequestsRetentionDays,
		BodyRetentionDays:     req.BodyRetentionDays,
		BodyCaptureEnabled:    req.BodyCaptureEnabled,
	}
	if err := s.Orgs.UpdateRetentionSettings(r.Context(), uc.OrgID, settings); err != nil {
		s.Logger.Error("api: failed to update settings", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, req)
}
