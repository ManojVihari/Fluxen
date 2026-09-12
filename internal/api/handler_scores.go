package api

import (
	"errors"
	"net/http"

	"fluxen/internal/store"
)

// scoreResponse mirrors store.Score. Overall/components are only
// meaningful when Status is "ok" — an "insufficient_data" row (or no row
// at all yet) still returns 200 with zeroed numbers, matching Part D.2's
// "never fabricate" convention applied elsewhere in the API.
type scoreResponse struct {
	Day     string `json:"day"`
	Status  string `json:"status"`
	Overall int    `json:"overall"`

	ModelEfficiency  int `json:"model_efficiency"`
	TokenEfficiency  int `json:"token_efficiency"`
	CacheEfficiency  int `json:"cache_efficiency"`
	TrafficStability int `json:"traffic_stability"`
	CostEfficiency   int `json:"cost_efficiency"`
}

func toScoreResponse(s store.Score) scoreResponse {
	return scoreResponse{
		Day: s.Day.Format("2006-01-02"), Status: s.Status, Overall: s.Overall,
		ModelEfficiency: s.ModelEfficiency, TokenEfficiency: s.TokenEfficiency, CacheEfficiency: s.CacheEfficiency,
		TrafficStability: s.TrafficStability, CostEfficiency: s.CostEfficiency,
	}
}

// handleApplicationScore returns an application's most recent Efficiency
// Score snapshot. Before the score.daily job has ever run for this
// application, there is no row yet — that's "not enough data" too, not
// an error, so it returns the same shape with a zeroed insufficient_data
// body rather than 404.
func (s *Server) handleApplicationScore(w http.ResponseWriter, r *http.Request) {
	appID, ok := s.mustOwnApplication(w, r)
	if !ok {
		return
	}

	latest, err := s.Scores.Latest(r.Context(), appID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusOK, scoreResponse{Status: "insufficient_data"})
			return
		}
		s.Logger.Error("api: failed to load application score", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toScoreResponse(latest))
}

// handleApplicationScoreHistory returns one snapshot per day in the
// range — the Efficiency tab's trend chart (default 30d, same ?range=
// convention every other rollup endpoint uses).
func (s *Server) handleApplicationScoreHistory(w http.ResponseWriter, r *http.Request) {
	appID, ok := s.mustOwnApplication(w, r)
	if !ok {
		return
	}

	since, until := parseRange(r)
	history, err := s.Scores.History(r.Context(), appID, since, until)
	if err != nil {
		s.Logger.Error("api: failed to load application score history", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := make([]scoreResponse, 0, len(history))
	for _, sc := range history {
		out = append(out, toScoreResponse(sc))
	}
	writeJSON(w, http.StatusOK, out)
}
