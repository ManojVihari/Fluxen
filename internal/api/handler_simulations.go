package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"fluxen/internal/sim"
	"fluxen/internal/store"
	"fluxen/pkg/types"
)

// EngineVersion is stamped on every simulation this build produces
// (simulations.engine_version) — bump it whenever internal/sim's replay
// or scenario math changes, so a stored result can be told apart from
// one produced by a different engine.
const EngineVersion = "sim-2026-09-01"

const defaultSimulationWindowDays = 14

type createSimulationRequest struct {
	AppID         string          `json:"app_id"`
	OpportunityID *string         `json:"opportunity_id,omitempty"`
	Scenario      json.RawMessage `json:"scenario"`
	WindowDays    int             `json:"window_days,omitempty"`
}

type scenarioEnvelope struct {
	Type string `json:"type"`
}

type modelMixScenarioRequest struct {
	CurrentModel   string  `json:"current_model"`
	CandidateModel string  `json:"candidate_model"`
	TrafficWeight  float64 `json:"traffic_weight"`
}

type cachingScenarioRequest struct {
	TTLSeconds int `json:"ttl_seconds"`
	MaxEntries int `json:"max_entries,omitempty"`
}

// simulationResponse mirrors store.Simulation. actual_cost_micro and the
// replay counts are measured straight from real historical rows;
// simulated_cost_micro, delta_micro, delta_pct, and
// projected_monthly_savings_micro are the hypothetical result of a
// scenario that never touched production (Rule 17, Part G.6) — hence one
// blanket value_type covering exactly those derived fields, the same
// minimal-tagging convention Phase 3's opportunity response uses.
type simulationResponse struct {
	ID            string  `json:"id"`
	AppID         string  `json:"app_id"`
	OpportunityID *string `json:"opportunity_id,omitempty"`

	Scenario json.RawMessage `json:"scenario"`

	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`

	ReplayedRequests int64 `json:"replayed_requests"`
	AffectedRequests int64 `json:"affected_requests"`
	Sampled          bool  `json:"sampled"`

	ActualCostMicro              int64   `json:"actual_cost_micro"`
	SimulatedCostMicro           int64   `json:"simulated_cost_micro"`
	DeltaMicro                   int64   `json:"delta_micro"`
	DeltaPct                     float64 `json:"delta_pct"`
	ProjectedMonthlySavingsMicro int64   `json:"projected_monthly_savings_micro"`
	ValueType                    string  `json:"value_type"`

	Breakdown   json.RawMessage `json:"breakdown"`
	Assumptions json.RawMessage `json:"assumptions"`

	EngineVersion string    `json:"engine_version"`
	CreatedAt     time.Time `json:"created_at"`
}

func toSimulationResponse(s store.Simulation) simulationResponse {
	return simulationResponse{
		ID: s.ID, AppID: string(s.AppID), OpportunityID: s.OpportunityID,
		Scenario:    s.Scenario,
		WindowStart: s.WindowStart, WindowEnd: s.WindowEnd,
		ReplayedRequests: s.ReplayedRequests, AffectedRequests: s.AffectedRequests, Sampled: s.Sampled,
		ActualCostMicro: s.ActualCostMicro, SimulatedCostMicro: s.SimulatedCostMicro,
		DeltaMicro: s.DeltaMicro, DeltaPct: s.DeltaPct, ProjectedMonthlySavingsMicro: s.ProjectedMonthlySavingsMicro,
		ValueType: "estimated",
		Breakdown: s.Breakdown, Assumptions: s.Assumptions,
		EngineVersion: s.EngineVersion, CreatedAt: s.CreatedAt,
	}
}

// handleCreateSimulation runs a scenario against real historical facts
// and persists the result — Phase 4's core deliverable (Part G.4).
// Computation is synchronous: at V1's replay scale (capped at 2M rows,
// Part G.4) this completes well within a normal request timeout, so
// there is no pending/polling state to model.
func (s *Server) handleCreateSimulation(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)

	var req createSimulationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	appID := types.AppID(req.AppID)
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

	windowStart, windowEnd, err := s.simulationWindow(r, uc.OrgID, appID, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	storeFacts, err := s.Requests.ReplayFacts(r.Context(), appID, windowStart, windowEnd)
	if err != nil {
		s.Logger.Error("api: failed to load replay facts", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	facts := make([]sim.ReplayFact, len(storeFacts))
	for i, f := range storeFacts {
		facts[i] = sim.ReplayFact{
			ID: f.ID, StartedAt: f.StartedAt,
			RequestedModel: f.RequestedModel, Provider: f.Provider, Model: f.Model,
			InputTokens: f.InputTokens, OutputTokens: f.OutputTokens,
			CostMicro: f.CostMicro, CostStatus: f.CostStatus, Status: f.Status, CacheKey: f.CacheKey,
		}
	}
	facts, sampled := sim.Cap(facts)

	var envelope scenarioEnvelope
	if err := json.Unmarshal(req.Scenario, &envelope); err != nil {
		writeError(w, http.StatusBadRequest, "invalid scenario")
		return
	}

	windowDays := windowEnd.Sub(windowStart).Hours() / 24

	var (
		affected, actualCost, simulatedCost int64
		breakdown, assumptions              any
	)

	switch envelope.Type {
	case "model_mix":
		var scenarioReq modelMixScenarioRequest
		if err := json.Unmarshal(req.Scenario, &scenarioReq); err != nil {
			writeError(w, http.StatusBadRequest, "invalid model_mix scenario")
			return
		}
		if scenarioReq.CurrentModel == "" || scenarioReq.CandidateModel == "" || scenarioReq.TrafficWeight <= 0 || scenarioReq.TrafficWeight > 1 {
			writeError(w, http.StatusBadRequest, "model_mix requires current_model, candidate_model, and traffic_weight in (0, 1]")
			return
		}
		result := sim.SimulateModelMix(facts, sim.ModelMixScenario{
			CurrentModel: scenarioReq.CurrentModel, CandidateModel: scenarioReq.CandidateModel, TrafficWeight: scenarioReq.TrafficWeight,
		}, s.Catalog)
		affected, actualCost, simulatedCost = result.AffectedRequests, result.ActualCostMicro, result.SimulatedCostMicro
		breakdown, assumptions = result.Breakdown, result.Assumptions

	case "exact_caching":
		var scenarioReq cachingScenarioRequest
		if err := json.Unmarshal(req.Scenario, &scenarioReq); err != nil {
			writeError(w, http.StatusBadRequest, "invalid exact_caching scenario")
			return
		}
		if scenarioReq.TTLSeconds <= 0 {
			writeError(w, http.StatusBadRequest, "exact_caching requires a positive ttl_seconds")
			return
		}
		result := sim.SimulateCaching(facts, sim.CachingScenario{
			TTL: time.Duration(scenarioReq.TTLSeconds) * time.Second, MaxEntries: scenarioReq.MaxEntries,
		})
		affected, actualCost, simulatedCost = result.AffectedRequests, result.ActualCostMicro, result.SimulatedCostMicro
		breakdown, assumptions = []struct{}{}, result.Assumptions

	default:
		writeError(w, http.StatusBadRequest, "unsupported scenario type")
		return
	}

	deltaMicro := simulatedCost - actualCost
	var deltaPct float64
	if actualCost != 0 {
		deltaPct = float64(deltaMicro) / float64(actualCost)
	}

	breakdownJSON, _ := json.Marshal(breakdown)
	assumptionsJSON, _ := json.Marshal(assumptions)

	var createdBy *string
	if uid := string(uc.UserID); uid != "" {
		createdBy = &uid
	}

	created, err := s.Simulations.Create(r.Context(), store.Simulation{
		OrgID: uc.OrgID, AppID: appID, OpportunityID: req.OpportunityID,
		Scenario:    req.Scenario,
		WindowStart: windowStart, WindowEnd: windowEnd,
		ReplayedRequests: int64(len(facts)), AffectedRequests: affected, Sampled: sampled,
		ActualCostMicro: actualCost, SimulatedCostMicro: simulatedCost,
		DeltaMicro: deltaMicro, DeltaPct: deltaPct,
		ProjectedMonthlySavingsMicro: sim.ProjectMonthly(windowDays, -deltaMicro),
		Breakdown:                    breakdownJSON, Assumptions: assumptionsJSON,
		EngineVersion: EngineVersion, CreatedBy: createdBy,
	})
	if err != nil {
		s.Logger.Error("api: failed to create simulation", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, toSimulationResponse(created))
}

// simulationWindow resolves the [start, end) window to replay: the
// referenced opportunity's own detection window if opportunity_id is
// given (so the simulation's "current cost" is directly comparable to
// that opportunity's own reported numbers), otherwise a trailing
// window_days (default 14) ending "now" — the same UTC start-of-tomorrow
// boundary internal/detect uses, so today's traffic is never silently
// excluded (the Phase 2 date-boundary bug this codebase already hit
// once).
func (s *Server) simulationWindow(r *http.Request, orgID types.OrgID, appID types.AppID, req createSimulationRequest) (time.Time, time.Time, error) {
	if req.OpportunityID != nil && *req.OpportunityID != "" {
		opp, err := s.Opportunities.Get(r.Context(), orgID, *req.OpportunityID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return time.Time{}, time.Time{}, errors.New("opportunity not found")
			}
			return time.Time{}, time.Time{}, err
		}
		if opp.AppID != appID {
			return time.Time{}, time.Time{}, errors.New("opportunity does not belong to app_id")
		}
		return opp.WindowStart, opp.WindowEnd, nil
	}

	days := req.WindowDays
	if days <= 0 {
		days = defaultSimulationWindowDays
	}
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	windowEnd := today.AddDate(0, 0, 1)
	windowStart := windowEnd.AddDate(0, 0, -days-1)
	return windowStart, windowEnd, nil
}

// handleGetSimulation returns one simulation's full result.
func (s *Server) handleGetSimulation(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)
	id := chi.URLParam(r, "simulationID")

	sim, err := s.Simulations.Get(r.Context(), uc.OrgID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "simulation not found")
			return
		}
		s.Logger.Error("api: failed to get simulation", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toSimulationResponse(sim))
}

// handleListApplicationSimulations returns an application's simulation
// history, newest first.
func (s *Server) handleListApplicationSimulations(w http.ResponseWriter, r *http.Request) {
	appID, ok := s.mustOwnApplication(w, r)
	if !ok {
		return
	}
	uc, _ := userFromRequest(r)

	sims, err := s.Simulations.ListByApp(r.Context(), uc.OrgID, appID)
	if err != nil {
		s.Logger.Error("api: failed to list simulations", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := make([]simulationResponse, 0, len(sims))
	for _, sr := range sims {
		out = append(out, toSimulationResponse(sr))
	}
	writeJSON(w, http.StatusOK, out)
}
