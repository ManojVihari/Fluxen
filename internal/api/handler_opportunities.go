package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"fluxen/internal/store"
	"fluxen/pkg/types"
)

// opportunityResponse mirrors store.Opportunity, passing Evidence and
// Recommendation through as raw JSON — the API never interprets a
// detector's evidence shape, it only serves whatever the detector
// produced (Part L Phase 3 frontend tasks: "rendered from the real
// evidence JSON, not mocked").
//
// current_cost_micro/projected_cost_micro/savings_micro are all
// normalized to a 30-day month from the real trailing-14-day detection
// window (window_start/window_end below) — value_type marks them
// "projected" rather than "measured" so the UI never presents a detector
// estimate as a recorded fact (Rule 17, Part G.6).
type opportunityResponse struct {
	ID       string  `json:"id"`
	AppID    string  `json:"app_id"`
	Kind     string  `json:"kind"`
	Status   string  `json:"status"`
	Severity *string `json:"severity,omitempty"`
	Title    string  `json:"title"`
	Summary  string  `json:"summary"`

	WindowStart    time.Time `json:"window_start"`
	WindowEnd      time.Time `json:"window_end"`
	SampleRequests int64     `json:"sample_requests"`

	CurrentCostMicro   int64   `json:"current_cost_micro"`
	ProjectedCostMicro int64   `json:"projected_cost_micro"`
	SavingsMicro       int64   `json:"savings_micro"`
	SavingsPct         float64 `json:"savings_pct"`
	ValueType          string  `json:"value_type"`

	Confidence      string  `json:"confidence"`
	ConfidenceScore float64 `json:"confidence_score"`

	Evidence       json.RawMessage `json:"evidence"`
	Recommendation json.RawMessage `json:"recommendation"`

	DetectorVersion string     `json:"detector_version"`
	DetectedAt      time.Time  `json:"detected_at"`
	ReviewedAt      *time.Time `json:"reviewed_at,omitempty"`
	DismissedAt     *time.Time `json:"dismissed_at,omitempty"`
	DismissReason   *string    `json:"dismiss_reason,omitempty"`
	LastSeenAt      time.Time  `json:"last_seen_at"`
}

func toOpportunityResponse(o store.Opportunity) opportunityResponse {
	return opportunityResponse{
		ID: o.ID, AppID: string(o.AppID), Kind: o.Kind, Status: o.Status, Severity: o.Severity,
		Title: o.Title, Summary: o.Summary,
		WindowStart: o.WindowStart, WindowEnd: o.WindowEnd, SampleRequests: o.SampleRequests,
		CurrentCostMicro: o.CurrentCostMicro, ProjectedCostMicro: o.ProjectedCostMicro,
		SavingsMicro: o.SavingsMicro, SavingsPct: o.SavingsPct, ValueType: "projected",
		Confidence: o.Confidence, ConfidenceScore: o.ConfidenceScore,
		Evidence: o.Evidence, Recommendation: o.Recommendation,
		DetectorVersion: o.DetectorVersion, DetectedAt: o.DetectedAt,
		ReviewedAt: o.ReviewedAt, DismissedAt: o.DismissedAt, DismissReason: o.DismissReason,
		LastSeenAt: o.LastSeenAt,
	}
}

// handleListOpportunities answers "what has Fluxen found" — optionally
// filtered to one status and/or one application (Part L Phase 3 API
// contract: GET /api/v1/opportunities?status=&app_id=).
func (s *Server) handleListOpportunities(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)

	status := r.URL.Query().Get("status")
	appID := types.AppID(r.URL.Query().Get("app_id"))

	if appID != "" {
		if _, err := s.Apps.Get(r.Context(), uc.OrgID, appID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "application not found")
				return
			}
			s.Logger.Error("api: failed to look up application", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	opps, err := s.Opportunities.ListByOrg(r.Context(), uc.OrgID, status, appID)
	if err != nil {
		s.Logger.Error("api: failed to list opportunities", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := make([]opportunityResponse, 0, len(opps))
	for _, o := range opps {
		out = append(out, toOpportunityResponse(o))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleGetOpportunity returns one opportunity's full detail — the
// Optimizations detail page's Why/Evidence/Impact sections read directly
// from this (Part L Phase 3 frontend tasks).
func (s *Server) handleGetOpportunity(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)
	id := chi.URLParam(r, "opportunityID")

	o, err := s.Opportunities.Get(r.Context(), uc.OrgID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "opportunity not found")
			return
		}
		s.Logger.Error("api: failed to get opportunity", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toOpportunityResponse(o))
}

// handleReviewOpportunity transitions open -> reviewed (Part G.1). The
// dashboard also calls this automatically on first view of the detail
// page; this endpoint just makes it possible to call explicitly too.
func (s *Server) handleReviewOpportunity(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)
	id := chi.URLParam(r, "opportunityID")

	o, err := s.Opportunities.MarkReviewed(r.Context(), uc.OrgID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "opportunity not found")
			return
		}
		s.Logger.Error("api: failed to review opportunity", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toOpportunityResponse(o))
}
