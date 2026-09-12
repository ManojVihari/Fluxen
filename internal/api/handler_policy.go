package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"fluxen/internal/policy"
	corepolicy "fluxen/pkg/policy"
)

// policyResponse mirrors internal/policy.Store's Record — the current
// document plus its version, so the editor (Part I.4) can send that
// version back as expected_version on save and detect a stale edit.
type policyResponse struct {
	AppID     string                    `json:"app_id"`
	Version   int                       `json:"version"`
	Document  corepolicy.PolicyDocument `json:"document"`
	UpdatedAt time.Time                 `json:"updated_at"`
}

func toPolicyResponse(rec policy.Record) policyResponse {
	return policyResponse{AppID: string(rec.AppID), Version: rec.Version, Document: rec.Document, UpdatedAt: rec.UpdatedAt}
}

// handleGetPolicy returns an application's current policy — an app that
// has never had one written gets version 0 and an empty document, not a
// 404 (Part L Phase 5: "an application with no policy document behaves
// exactly like Phase 1's pipeline").
func (s *Server) handleGetPolicy(w http.ResponseWriter, r *http.Request) {
	appID, ok := s.mustOwnApplication(w, r)
	if !ok {
		return
	}
	rec, err := s.Policies.Get(r.Context(), appID)
	if err != nil {
		s.Logger.Error("api: failed to get policy", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toPolicyResponse(rec))
}

type putPolicyRequest struct {
	Document        corepolicy.PolicyDocument `json:"document"`
	ExpectedVersion int                       `json:"expected_version"`
	Note            string                    `json:"note,omitempty"`
}

// handlePutPolicy is the Policies editor's save action (Part I.4): full
// document replacement, diff-before-save already done client-side (the
// editor fetched the current document first), optimistic-concurrency
// checked via expected_version.
func (s *Server) handlePutPolicy(w http.ResponseWriter, r *http.Request) {
	appID, ok := s.mustOwnApplication(w, r)
	if !ok {
		return
	}
	uc, _ := userFromRequest(r)

	var req putPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := req.Document.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	current, err := s.Policies.Get(r.Context(), appID)
	if err != nil {
		s.Logger.Error("api: failed to load current policy", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	diff, err := policy.Diff(&current.Document, &req.Document)
	if err != nil {
		s.Logger.Error("api: failed to compute policy diff", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var notePtr *string
	if req.Note != "" {
		notePtr = &req.Note
	}
	userID := uc.UserID

	saved, err := s.Policies.Save(r.Context(), appID, req.ExpectedVersion, req.Document, policy.HistoryEntry{
		ChangeSource: "user", Note: notePtr, ChangedBy: &userID, Diff: diff,
	})
	if err != nil {
		if errors.Is(err, policy.ErrVersionConflict) {
			writeError(w, http.StatusConflict, "policy was changed by someone else — reload and try again")
			return
		}
		s.Logger.Error("api: failed to save policy", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if s.PolicySnapshot != nil {
		s.PolicySnapshot.Invalidate(r.Context(), appID)
	}

	writeJSON(w, http.StatusOK, toPolicyResponse(saved))
}

type policyHistoryResponse struct {
	ID            string                    `json:"id"`
	Version       int                       `json:"version"`
	Document      corepolicy.PolicyDocument `json:"document"`
	Diff          json.RawMessage           `json:"diff"`
	ChangeSource  string                    `json:"change_source"`
	OpportunityID *string                   `json:"opportunity_id,omitempty"`
	SimulationID  *string                   `json:"simulation_id,omitempty"`
	Note          *string                   `json:"note,omitempty"`
	ChangedAt     time.Time                 `json:"changed_at"`
}

// handlePolicyHistory returns every mutation ever made to an
// application's policy, newest first (Part I.4: "writes to history").
func (s *Server) handlePolicyHistory(w http.ResponseWriter, r *http.Request) {
	appID, ok := s.mustOwnApplication(w, r)
	if !ok {
		return
	}
	history, err := s.Policies.History(r.Context(), appID)
	if err != nil {
		s.Logger.Error("api: failed to load policy history", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := make([]policyHistoryResponse, 0, len(history))
	for _, h := range history {
		out = append(out, policyHistoryResponse{
			ID: h.ID, Version: h.Version, Document: h.Document, Diff: h.Diff,
			ChangeSource: h.ChangeSource, OpportunityID: h.OpportunityID, SimulationID: h.SimulationID,
			Note: h.Note, ChangedAt: h.ChangedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

type revertPolicyRequest struct {
	Version int    `json:"version"`
	Note    string `json:"note,omitempty"`
}

// handleRevertPolicy restores a prior version's document as a brand-new
// version (never rewrites history — Part E.1's policy_history is
// append-only) with change_source="revert".
func (s *Server) handleRevertPolicy(w http.ResponseWriter, r *http.Request) {
	appID, ok := s.mustOwnApplication(w, r)
	if !ok {
		return
	}
	uc, _ := userFromRequest(r)

	var req revertPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	target, err := s.Policies.AtVersion(r.Context(), appID, req.Version)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such policy version")
		return
	}

	current, err := s.Policies.Get(r.Context(), appID)
	if err != nil {
		s.Logger.Error("api: failed to load current policy", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	diff, err := policy.Diff(&current.Document, &target)
	if err != nil {
		s.Logger.Error("api: failed to compute policy diff", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var notePtr *string
	if req.Note != "" {
		notePtr = &req.Note
	}
	userID := uc.UserID

	saved, err := s.Policies.Save(r.Context(), appID, current.Version, target, policy.HistoryEntry{
		ChangeSource: "revert", Note: notePtr, ChangedBy: &userID, Diff: diff,
	})
	if err != nil {
		if errors.Is(err, policy.ErrVersionConflict) {
			writeError(w, http.StatusConflict, "policy was changed by someone else — reload and try again")
			return
		}
		s.Logger.Error("api: failed to revert policy", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if s.PolicySnapshot != nil {
		s.PolicySnapshot.Invalidate(r.Context(), appID)
	}

	writeJSON(w, http.StatusOK, toPolicyResponse(saved))
}
