package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"fluxen/internal/policy"
	"fluxen/internal/store"
	"fluxen/pkg/types"
)

// measurementResponse mirrors store.Measurement. Baseline/observed
// figures are "measured" (summed straight from real requests);
// actual_savings_micro/actual_pct are "realized" only once status is
// final — Part G.6's fourth value type, the one this phase makes real.
// expected_savings_micro/expected_pct stay "estimated" (carried over
// from the opportunity at apply time) and are never summed into
// realized-savings totals anywhere (Part G.5).
type measurementResponse struct {
	ID            string    `json:"id"`
	AppID         string    `json:"app_id"`
	OpportunityID string    `json:"opportunity_id"`
	SimulationID  *string   `json:"simulation_id,omitempty"`
	PolicyVersion int       `json:"policy_version"`
	AppliedAt     time.Time `json:"applied_at"`

	BaselineStart          time.Time `json:"baseline_start"`
	BaselineEnd            time.Time `json:"baseline_end"`
	BaselineRequests       int64     `json:"baseline_requests"`
	BaselineCostMicro      int64     `json:"baseline_cost_micro"`
	BaselineCostPer1kMicro int64     `json:"baseline_cost_per_1k_micro"`

	ObservedStart          *time.Time `json:"observed_start,omitempty"`
	ObservedEnd            *time.Time `json:"observed_end,omitempty"`
	ObservedRequests       *int64     `json:"observed_requests,omitempty"`
	ObservedCostMicro      *int64     `json:"observed_cost_micro,omitempty"`
	ObservedCostPer1kMicro *int64     `json:"observed_cost_per_1k_micro,omitempty"`

	ExpectedSavingsMicro int64    `json:"expected_savings_micro"`
	ActualSavingsMicro   *int64   `json:"actual_savings_micro,omitempty"`
	ExpectedPct          float64  `json:"expected_pct"`
	ActualPct            *float64 `json:"actual_pct,omitempty"`

	Verdict       *string `json:"verdict,omitempty"`
	VerdictReason *string `json:"verdict_reason,omitempty"`

	Status      string     `json:"status"`
	FinalizedAt *time.Time `json:"finalized_at,omitempty"`
}

func toMeasurementResponse(m store.Measurement) measurementResponse {
	return measurementResponse{
		ID: m.ID, AppID: string(m.AppID), OpportunityID: m.OpportunityID, SimulationID: m.SimulationID,
		PolicyVersion: m.PolicyVersion, AppliedAt: m.AppliedAt,
		BaselineStart: m.BaselineStart, BaselineEnd: m.BaselineEnd,
		BaselineRequests: m.BaselineRequests, BaselineCostMicro: m.BaselineCostMicro, BaselineCostPer1kMicro: m.BaselineCostPer1kMicro,
		ObservedStart: m.ObservedStart, ObservedEnd: m.ObservedEnd,
		ObservedRequests: m.ObservedRequests, ObservedCostMicro: m.ObservedCostMicro, ObservedCostPer1kMicro: m.ObservedCostPer1kMicro,
		ExpectedSavingsMicro: m.ExpectedSavingsMicro, ActualSavingsMicro: m.ActualSavingsMicro,
		ExpectedPct: m.ExpectedPct, ActualPct: m.ActualPct,
		Verdict: m.Verdict, VerdictReason: m.VerdictReason,
		Status: m.Status, FinalizedAt: m.FinalizedAt,
	}
}

// handleListMeasurements returns an application's measurement history,
// newest first — GET /api/v1/measurements?app_id=.
func (s *Server) handleListMeasurements(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)
	appID := types.AppID(r.URL.Query().Get("app_id"))
	if appID == "" {
		writeError(w, http.StatusBadRequest, "app_id is required")
		return
	}
	if _, err := s.Apps.Get(r.Context(), uc.OrgID, appID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "application not found")
			return
		}
		s.Logger.Error("api: failed to look up application", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	measurements, err := s.Measurements.ListByApp(r.Context(), uc.OrgID, appID)
	if err != nil {
		s.Logger.Error("api: failed to list measurements", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := make([]measurementResponse, 0, len(measurements))
	for _, m := range measurements {
		out = append(out, toMeasurementResponse(m))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleGetMeasurement returns one measurement's full result.
func (s *Server) handleGetMeasurement(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)
	id := chi.URLParam(r, "measurementID")

	m, err := s.Measurements.Get(r.Context(), uc.OrgID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "measurement not found")
			return
		}
		s.Logger.Error("api: failed to get measurement", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toMeasurementResponse(m))
}

// handleGetOpportunityMeasurement returns the live measurement for one
// opportunity, if any — the Optimizations detail page's Measure section
// (Part I.2) reads this directly rather than listing by app and
// filtering client-side.
func (s *Server) handleGetOpportunityMeasurement(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)
	opportunityID := chi.URLParam(r, "opportunityID")

	m, err := s.Measurements.GetByOpportunity(r.Context(), uc.OrgID, opportunityID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no measurement for this opportunity")
			return
		}
		s.Logger.Error("api: failed to get opportunity measurement", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toMeasurementResponse(m))
}

type revertMeasurementRequest struct {
	Confirm bool   `json:"confirm"`
	Note    string `json:"note,omitempty"`
}

// handleRevertMeasurement is the one-click Revert a `regressed` verdict
// surfaces (Part G.5): restores the exact pre-apply policy document as a
// new version, and marks both the opportunity and this measurement
// reverted. Requires confirm: true (Rule 19 — no autonomous mutation,
// and revert is itself a production change).
func (s *Server) handleRevertMeasurement(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)
	id := chi.URLParam(r, "measurementID")

	var req revertMeasurementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !req.Confirm {
		writeError(w, http.StatusBadRequest, "confirm must be true to revert a policy change")
		return
	}

	userID := uc.UserID
	reverter := &policy.Reverter{Policies: s.Policies, Opportunities: s.Opportunities, Measurements: s.Measurements, Snapshot: s.PolicySnapshot}
	rec, err := reverter.Revert(r.Context(), uc.OrgID, id, &userID, req.Note)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "measurement not found")
			return
		}
		s.Logger.Error("api: failed to revert measurement", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, toPolicyResponse(rec))
}
