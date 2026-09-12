package api

import (
	"net/http"
	"sort"

	"fluxen/internal/store"
)

// overviewOpportunityRow is a trimmed opportunity shape for the
// Overview page's feed (Part I.3: "top 5 by savings × confidence, each
// with a Review button linking to I.2") — the full Opportunity shape
// belongs to GET /opportunities/{id}; this feed only needs enough to
// render a card and link out.
type overviewOpportunityRow struct {
	ID              string  `json:"id"`
	AppID           string  `json:"app_id"`
	Kind            string  `json:"kind"`
	Title           string  `json:"title"`
	SavingsMicro    int64   `json:"savings_micro"`
	SavingsPct      float64 `json:"savings_pct"`
	Confidence      string  `json:"confidence"`
	ConfidenceScore float64 `json:"confidence_score"`
}

type topApplicationResponse struct {
	AppID                string `json:"app_id"`
	Slug                 string `json:"slug"`
	Name                 string `json:"name"`
	CostMicro            int64  `json:"cost_micro"`
	PriorCostMicro       int64  `json:"prior_cost_micro"`
	EfficiencyScore      *int   `json:"efficiency_score"`
	OpenOpportunityValue int64  `json:"open_opportunity_value_micro"`
}

type providerMixResponse struct {
	Provider  string `json:"provider"`
	CostMicro int64  `json:"cost_micro"`
	Requests  int64  `json:"requests"`
}

// handleOverview answers Part I.3's single combined payload: header
// numbers, potential vs. realized savings, provider mix, top
// applications, and the opportunity feed — one call so the page renders
// without a waterfall of requests.
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)
	since, until := parseRange(r)
	priorSince := since.Add(-until.Sub(since))

	summary, err := s.Overview.Summary(r.Context(), uc.OrgID, since, until)
	if err != nil {
		s.Logger.Error("api: failed to load overview summary", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	mix, err := s.Overview.ProviderMix(r.Context(), uc.OrgID, since, until)
	if err != nil {
		s.Logger.Error("api: failed to load provider mix", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	mixOut := make([]providerMixResponse, 0, len(mix))
	for _, m := range mix {
		mixOut = append(mixOut, providerMixResponse{Provider: m.Provider, CostMicro: int64(m.CostMicro), Requests: m.Requests})
	}

	top, err := s.Overview.TopApplications(r.Context(), uc.OrgID, since, until, priorSince, 10)
	if err != nil {
		s.Logger.Error("api: failed to load top applications", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	topOut := make([]topApplicationResponse, 0, len(top))
	for _, a := range top {
		topOut = append(topOut, topApplicationResponse{
			AppID: string(a.AppID), Slug: a.Slug, Name: a.Name,
			CostMicro: int64(a.CostMicro), PriorCostMicro: int64(a.PriorCostMicro),
			EfficiencyScore: a.EfficiencyScore, OpenOpportunityValue: a.OpenOpportunityValue,
		})
	}

	// Potential savings (estimated, Part G.6) and the opportunity feed
	// both derive from the org-wide "live" opportunity set — open,
	// reviewed, or simulated (Part G.1's own definition of not-yet-
	// resolved, the same set store.Opportunities.MarkApplied treats as
	// applicable) — not just literal status="open", so a reviewed
	// opportunity doesn't silently vanish from the org's own savings
	// total. Part G.2 already caps this at 5 per application, so pulling
	// every row and filtering/ranking in memory is cheap even for a
	// many-application org.
	allOpps, err := s.Opportunities.ListByOrg(r.Context(), uc.OrgID, "", "")
	if err != nil {
		s.Logger.Error("api: failed to load opportunities for overview", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var openOpps []store.Opportunity
	for _, o := range allOpps {
		if o.Status == "open" || o.Status == "reviewed" || o.Status == "simulated" {
			openOpps = append(openOpps, o)
		}
	}
	var potentialSavingsMicro int64
	for _, o := range openOpps {
		potentialSavingsMicro += o.SavingsMicro
	}
	sort.SliceStable(openOpps, func(i, j int) bool {
		return float64(openOpps[i].SavingsMicro)*openOpps[i].ConfidenceScore > float64(openOpps[j].SavingsMicro)*openOpps[j].ConfidenceScore
	})
	feedLimit := 5
	if len(openOpps) < feedLimit {
		feedLimit = len(openOpps)
	}
	feed := make([]overviewOpportunityRow, 0, feedLimit)
	for _, o := range openOpps[:feedLimit] {
		feed = append(feed, overviewOpportunityRow{
			ID: o.ID, AppID: string(o.AppID), Kind: o.Kind, Title: o.Title,
			SavingsMicro: o.SavingsMicro, SavingsPct: o.SavingsPct,
			Confidence: o.Confidence, ConfidenceScore: o.ConfidenceScore,
		})
	}

	realizedSavingsMicro, err := s.Measurements.RealizedSavings(r.Context(), uc.OrgID)
	if err != nil {
		s.Logger.Error("api: failed to load realized savings for overview", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"range_start": since.Format("2006-01-02"), "range_end": until.Format("2006-01-02"),
		"requests": summary.Requests, "errors": summary.Errors,
		"input_tokens": summary.InputTokens, "output_tokens": summary.OutputTokens, "total_tokens": summary.TotalTokens,
		"cost_micro":              int64(summary.CostMicro),
		"potential_savings_micro": potentialSavingsMicro,
		"realized_savings_micro":  realizedSavingsMicro,
		"provider_mix":            mixOut,
		"top_applications":        topOut,
		"opportunity_feed":        feed,
	})
}

type overviewDailyPointResponse struct {
	Day       string `json:"day"`
	CostMicro int64  `json:"cost_micro"`
	Requests  int64  `json:"requests"`
}

// handleOverviewTimeseries returns the org-wide cost-over-time series
// (Part I.3's "cost-over-time stacked by provider" chart consumes this
// for the total line; the provider breakdown itself is a per-range
// snapshot from handleOverview's provider_mix, not a second timeseries —
// stacking by provider over time is a frontend-only nice-to-have this
// pass does not add a second query for).
func (s *Server) handleOverviewTimeseries(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)
	since, until := parseRange(r)

	points, err := s.Overview.Timeseries(r.Context(), uc.OrgID, since, until)
	if err != nil {
		s.Logger.Error("api: failed to load overview timeseries", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := make([]overviewDailyPointResponse, 0, len(points))
	for _, p := range points {
		out = append(out, overviewDailyPointResponse{Day: p.Day.Format("2006-01-02"), CostMicro: int64(p.CostMicro), Requests: p.Requests})
	}
	writeJSON(w, http.StatusOK, out)
}
