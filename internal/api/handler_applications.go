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

type createApplicationRequest struct {
	Name string `json:"name"`
}

type applicationResponse struct {
	ID          string     `json:"id"`
	Slug        string     `json:"slug"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	FirstSeenAt *time.Time `json:"first_seen_at,omitempty"`
	LastSeenAt  *time.Time `json:"last_seen_at,omitempty"`
}

func toApplicationResponse(a store.Application) applicationResponse {
	return applicationResponse{
		ID: string(a.ID), Slug: a.Slug, Name: a.Name, Status: a.Status,
		CreatedAt: a.CreatedAt, FirstSeenAt: a.FirstSeenAt, LastSeenAt: a.LastSeenAt,
	}
}

// handleCreateApplication creates an application, deriving its slug from
// the name (Part L Phase 1: "slug auto-generated, editable" — editing
// comes with the fuller Application Detail screen in Phase 2; Phase 1
// only needs a working, unique slug at creation time).
func (s *Server) handleCreateApplication(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)

	var req createApplicationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	slug, err := s.Apps.UniqueSlug(r.Context(), uc.OrgID, req.Name)
	if err != nil {
		s.Logger.Error("api: failed to generate slug", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	app, err := s.Apps.Create(r.Context(), uc.OrgID, slug, req.Name)
	if err != nil {
		s.Logger.Error("api: failed to create application", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, toApplicationResponse(app))
}

func (s *Server) handleListApplications(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)

	apps, err := s.Apps.ListByOrg(r.Context(), uc.OrgID)
	if err != nil {
		s.Logger.Error("api: failed to list applications", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := make([]applicationResponse, 0, len(apps))
	for _, a := range apps {
		out = append(out, toApplicationResponse(a))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleArchiveApplication and handleUnarchiveApplication toggle an
// application's status (Part L: "delete or archive" — archive was chosen
// over a hard delete since an application's historical rollups,
// opportunities, and policy history all reference it by id, and none of
// that should silently disappear just because someone tidied up their
// applications list). Archiving is purely a visibility change in the
// dashboard's default list; it neither revokes API keys nor stops the
// gateway from routing the application's traffic.
func (s *Server) handleArchiveApplication(w http.ResponseWriter, r *http.Request) {
	s.setApplicationStatus(w, r, "archived")
}

func (s *Server) handleUnarchiveApplication(w http.ResponseWriter, r *http.Request) {
	s.setApplicationStatus(w, r, "active")
}

func (s *Server) setApplicationStatus(w http.ResponseWriter, r *http.Request, status string) {
	uc, _ := userFromRequest(r)
	appID := types.AppID(chi.URLParam(r, "appID"))

	app, err := s.Apps.UpdateStatus(r.Context(), uc.OrgID, appID, status)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "application not found")
			return
		}
		s.Logger.Error("api: failed to update application status", "error", err, "status", status)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toApplicationResponse(app))
}

// summaryResponse mirrors store.ApplicationSummary. Money stays a plain
// int64 of micro-USD — the dashboard's <Money> convention (Part I.7)
// arrives with the UI that renders it; the wire format is stable either
// way.
type summaryResponse struct {
	RangeStart    time.Time `json:"range_start"`
	RangeEnd      time.Time `json:"range_end"`
	Requests      int64     `json:"requests"`
	Errors        int64     `json:"errors"`
	InputTokens   int64     `json:"input_tokens"`
	OutputTokens  int64     `json:"output_tokens"`
	TotalTokens   int64     `json:"total_tokens"`
	CostMicro     int64     `json:"cost_micro"`
	AvgDurationMS float64   `json:"avg_duration_ms"`
}

// handleApplicationSummary answers Phase 2's core question for one
// application: what is it doing and spending, over ?range= (default 30d).
func (s *Server) handleApplicationSummary(w http.ResponseWriter, r *http.Request) {
	appID, ok := s.mustOwnApplication(w, r)
	if !ok {
		return
	}

	since, now := parseRange(r)
	summary, err := s.Rollups.Summary(r.Context(), appID, since, now)
	if err != nil {
		s.Logger.Error("api: failed to load application summary", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, summaryResponse{
		RangeStart: summary.RangeStart, RangeEnd: summary.RangeEnd,
		Requests: summary.Requests, Errors: summary.Errors,
		InputTokens: summary.InputTokens, OutputTokens: summary.OutputTokens, TotalTokens: summary.TotalTokens,
		CostMicro: int64(summary.CostMicro), AvgDurationMS: summary.AvgDurationMS,
	})
}

type dailyPointResponse struct {
	Day           string  `json:"day"`
	Requests      int64   `json:"requests"`
	Errors        int64   `json:"errors"`
	InputTokens   int64   `json:"input_tokens"`
	OutputTokens  int64   `json:"output_tokens"`
	TotalTokens   int64   `json:"total_tokens"`
	CostMicro     int64   `json:"cost_micro"`
	AvgDurationMS float64 `json:"avg_duration_ms"`
}

// handleApplicationTimeseries returns one point per day in the range —
// the UI derives whatever line chart it wants (cost, requests, tokens)
// from this one payload rather than needing a separate call per metric.
func (s *Server) handleApplicationTimeseries(w http.ResponseWriter, r *http.Request) {
	appID, ok := s.mustOwnApplication(w, r)
	if !ok {
		return
	}

	since, now := parseRange(r)
	points, err := s.Rollups.Timeseries(r.Context(), appID, since, now)
	if err != nil {
		s.Logger.Error("api: failed to load application timeseries", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := make([]dailyPointResponse, 0, len(points))
	for _, p := range points {
		out = append(out, dailyPointResponse{
			Day:      p.Day.Format("2006-01-02"),
			Requests: p.Requests, Errors: p.Errors,
			InputTokens: p.InputTokens, OutputTokens: p.OutputTokens, TotalTokens: p.TotalTokens,
			CostMicro: int64(p.CostMicro), AvgDurationMS: p.AvgDurationMS,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

type modelBreakdownResponse struct {
	Provider      string  `json:"provider"`
	Model         string  `json:"model"`
	Requests      int64   `json:"requests"`
	Errors        int64   `json:"errors"`
	InputTokens   int64   `json:"input_tokens"`
	OutputTokens  int64   `json:"output_tokens"`
	TotalTokens   int64   `json:"total_tokens"`
	CostMicro     int64   `json:"cost_micro"`
	AvgDurationMS float64 `json:"avg_duration_ms"`
}

// handleApplicationModels answers "which models/providers are
// responsible" — the model-mix breakdown Application Detail's Models tab
// renders, sorted by cost descending.
func (s *Server) handleApplicationModels(w http.ResponseWriter, r *http.Request) {
	appID, ok := s.mustOwnApplication(w, r)
	if !ok {
		return
	}

	since, now := parseRange(r)
	breakdown, err := s.Rollups.ModelBreakdown(r.Context(), appID, since, now)
	if err != nil {
		s.Logger.Error("api: failed to load model breakdown", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := make([]modelBreakdownResponse, 0, len(breakdown))
	for _, m := range breakdown {
		out = append(out, modelBreakdownResponse{
			Provider: m.Provider, Model: m.Model,
			Requests: m.Requests, Errors: m.Errors,
			InputTokens: m.InputTokens, OutputTokens: m.OutputTokens, TotalTokens: m.TotalTokens,
			CostMicro: int64(m.CostMicro), AvgDurationMS: m.AvgDurationMS,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// mustOwnApplication resolves the {appID} URL param and confirms it
// belongs to the caller's org, writing the appropriate error response and
// returning ok=false if not. Every per-application rollup endpoint starts
// with this same org-scoping check (Part E: "one org can never read
// another's application").
func (s *Server) mustOwnApplication(w http.ResponseWriter, r *http.Request) (types.AppID, bool) {
	uc, _ := userFromRequest(r)
	appID := types.AppID(chi.URLParam(r, "appID"))

	if _, err := s.Apps.Get(r.Context(), uc.OrgID, appID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "application not found")
			return "", false
		}
		s.Logger.Error("api: failed to look up application", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return "", false
	}
	return appID, true
}
