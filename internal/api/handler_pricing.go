package api

import "net/http"

// pricingModelResponse mirrors pricing.ModelPrice — Settings > Pricing's
// read-only catalog view (Part I.6: "view catalog, view Ollama local-
// cost label — no GPU cost fields"). Per-org editable price overrides
// are named in the same spec line ("view/edit org overrides") but
// nowhere else in the specification: no data-model entry, no API
// contract, no rule for how an override would interact with
// pricing_version/backfill (Part H.2's "catalog updates never rewrite
// history" is specifically about the shared catalog, and doesn't say
// what an org-specific override would do to that guarantee). That's a
// real, undefined feature, not a small gap — this endpoint deliberately
// ships the unambiguous half (a read-only catalog view) and leaves
// override editing out rather than inventing its data model here.
type pricingModelResponse struct {
	ID                     string   `json:"id"`
	Provider               string   `json:"provider"`
	InputPerMTokMicro      int64    `json:"input_per_mtok_micro"`
	OutputPerMTokMicro     int64    `json:"output_per_mtok_micro"`
	ContextWindow          int      `json:"context_window"`
	MaxOutput              int      `json:"max_output"`
	Capabilities           []string `json:"capabilities"`
	Tier                   string   `json:"tier"`
	DowngradeCandidatesFor []string `json:"downgrade_candidates_for,omitempty"`
}

type pricingCatalogResponse struct {
	Version string                 `json:"version"`
	Models  []pricingModelResponse `json:"models"`
	// OllamaNote documents Part D.1's frozen decision inline, so the
	// Settings screen can render it next to the catalog without a
	// separate call: Ollama has no catalog entries by design.
	OllamaNote string `json:"ollama_note"`
}

// handleGetPricingCatalog returns the embedded pricing catalog — Part
// H.1's versioned, human-edited price list, read-only from the API (the
// only way to change it is to ship a new catalog.yaml and redeploy,
// matching Part H.2's "catalog updates ... never automatic").
func (s *Server) handleGetPricingCatalog(w http.ResponseWriter, r *http.Request) {
	out := pricingCatalogResponse{
		Version: s.Catalog.Version,
		OllamaNote: "Ollama has no catalog entries. Every Ollama request is priced as " +
			"\"Local · not billed\" (cost_status=local) — there is no GPU/electricity cost model in V1.",
	}
	for _, m := range s.Catalog.AllModels() {
		out.Models = append(out.Models, pricingModelResponse{
			ID: m.ID, Provider: m.Provider,
			InputPerMTokMicro: m.InputPerMTokMicro, OutputPerMTokMicro: m.OutputPerMTokMicro,
			ContextWindow: m.ContextWindow, MaxOutput: m.MaxOutput,
			Capabilities: m.Capabilities, Tier: m.Tier, DowngradeCandidatesFor: m.DowngradeCandidatesFor,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
