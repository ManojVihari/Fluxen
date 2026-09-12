package detect

import (
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"fluxen/internal/sim"
	"fluxen/pkg/types"
)

// RepeatedRequestDetectorVersion is stamped on every candidate this
// detector produces.
const RepeatedRequestDetectorVersion = "repeated-request-2026-09-01"

// repeatedRequestWindowDays is Part G.3.2's fixed detection window:
// "Window: trailing 7 days."
const repeatedRequestWindowDays = 7

// duplicateRateFloor is Part G.3.2's own minimum, in addition to the
// global floors (Part G.2): "duplicate_rate ≥ 0.05."
const duplicateRateFloor = 0.05

// repeatedRequestHaircut is Part G.3.2's stated real-world discount:
// "savings = duplicate_cost_in_ttl × 0.9 (10% haircut for real-world
// eviction/cold-start effects)."
const repeatedRequestHaircut = 0.9

// repeatedRequestMonthlyScale normalizes the 7-day window to 30 days
// (Part G.3.2: "normalized to 30 days" — this detector's own window is
// shorter than Model Cost's 14 days, so its scale factor differs).
const repeatedRequestMonthlyScale = 30.0 / float64(repeatedRequestWindowDays)

// ttlSweep is Part G.3.2's fixed sweep: "TTL sweep evaluated at
// 5m / 1h / 6h / 24h."
var ttlSweep = []struct {
	Seconds int
	Label   string
}{
	{300, "5m"},
	{3600, "1h"},
	{21600, "6h"},
	{86400, "24h"},
}

// RepeatedRequestFact is the minimal per-request shape this detector
// reads — deliberately not the full requests row, so this file's own
// logic is unit-testable against a hand-built fixture without a
// database (Rule 8). internal/detect's Runner maps
// store.RepeatedRequestFact into this shape.
type RepeatedRequestFact struct {
	StartedAt        time.Time
	CacheKey         []byte
	CostMicro        int64
	CostStatus       types.CostStatus
	RequestedModel   string
	InputTokens      int
	SystemPromptHash []byte
}

// TTLSweepRow is one row of the evidence's TTL sweep table — "the
// evidence shows the savings curve across all four so the user can
// choose" (Part G.3.2).
type TTLSweepRow struct {
	TTL           string `json:"ttl"`
	DuplicateHits int64  `json:"duplicate_hits"`
	SavingsMicro  int64  `json:"savings_micro"`
}

// DuplicateShape is one of the "top duplicated request shapes" (Part
// G.3.2: "identified by system_prompt_hash + token profile, never raw
// prompt text").
type DuplicateShape struct {
	SystemPromptHash string `json:"system_prompt_hash"`
	Model            string `json:"model"`
	InputTokensP50   int    `json:"input_tokens_p50"`
	Count            int64  `json:"count"`
}

// RepeatedRequestEvidence is the evidence JSON stored on a repeated-
// request opportunity.
type RepeatedRequestEvidence struct {
	TotalRequests     int64            `json:"total_requests"`
	DuplicateRequests int64            `json:"duplicate_requests"`
	DuplicateRate     float64          `json:"duplicate_rate"`
	TTLSweep          []TTLSweepRow    `json:"ttl_sweep"`
	TopShapes         []DuplicateShape `json:"top_shapes"`
	WindowDays        int              `json:"window_days"`
}

// RepeatedRequestRecommendation is the recommendation JSON — shaped for
// a future Apply flow to enable exact caching at the chosen TTL (the
// enforcement itself, internal/cache, already exists since Phase 5;
// only the opportunity → apply wiring for this specific kind is a later
// increment, matching Phase 3's own model-cost-only Apply scope).
type RepeatedRequestRecommendation struct {
	Action                string `json:"action"`
	RecommendedTTLSeconds int    `json:"recommended_ttl_seconds"`
}

// DetectRepeatedRequest is the Repeated Request detector (Part G.3.2) —
// a pure function of a fact slice. It replays the exact same TTL sweep
// through internal/sim.SimulateCaching that Phase 4's exact-caching
// simulation uses (Rule 9-adjacent: never reimplement the cache
// replay logic a second time), so a detected opportunity's numbers are
// always reproducible by running that same simulation by hand.
func DetectRepeatedRequest(facts []RepeatedRequestFact, windowStart, windowEnd time.Time, appID types.AppID) []Candidate {
	known := make([]RepeatedRequestFact, 0, len(facts))
	for _, f := range facts {
		if f.CostStatus == types.CostKnown {
			known = append(known, f)
		}
	}
	total := int64(len(known))
	if total == 0 {
		return nil
	}

	seen := make(map[string]bool, total)
	var duplicates int64
	for _, f := range known {
		key := string(f.CacheKey)
		if seen[key] {
			duplicates++
		}
		seen[key] = true
	}
	duplicateRate := float64(duplicates) / float64(total)
	if duplicateRate < duplicateRateFloor {
		return nil
	}

	simFacts := make([]sim.ReplayFact, len(known))
	var currentCostAll int64
	for i, f := range known {
		simFacts[i] = sim.ReplayFact{StartedAt: f.StartedAt, CacheKey: f.CacheKey, CostMicro: f.CostMicro, CostStatus: f.CostStatus, Status: "ok"}
		currentCostAll += f.CostMicro
	}

	var sweep []TTLSweepRow
	var bestTTLSeconds int
	var bestSavingsMonthly int64
	for _, ttl := range ttlSweep {
		result := sim.SimulateCaching(simFacts, sim.CachingScenario{TTL: time.Duration(ttl.Seconds) * time.Second})
		savingsWindow := float64(result.ActualCostMicro-result.SimulatedCostMicro) * repeatedRequestHaircut
		savingsMonthly := round(savingsWindow * repeatedRequestMonthlyScale)
		sweep = append(sweep, TTLSweepRow{TTL: ttl.Label, DuplicateHits: result.AffectedRequests, SavingsMicro: savingsMonthly})
		if savingsMonthly > bestSavingsMonthly {
			bestSavingsMonthly = savingsMonthly
			bestTTLSeconds = ttl.Seconds
		}
	}
	if bestSavingsMonthly <= 0 {
		return nil
	}

	currentCostMonthly := round(float64(currentCostAll) * repeatedRequestMonthlyScale)
	projectedCostMonthly := currentCostMonthly - bestSavingsMonthly
	var savingsPct float64
	if currentCostMonthly > 0 {
		savingsPct = float64(bestSavingsMonthly) / float64(currentCostMonthly)
	}

	confidence := repeatedRequestConfidence(duplicateRate, total)
	topShapes := topDuplicateShapes(known)

	evidence := RepeatedRequestEvidence{
		TotalRequests: total, DuplicateRequests: duplicates, DuplicateRate: duplicateRate,
		TTLSweep: sweep, TopShapes: topShapes, WindowDays: repeatedRequestWindowDays,
	}
	recommendation := RepeatedRequestRecommendation{Action: "enable_caching", RecommendedTTLSeconds: bestTTLSeconds}

	pct := int(duplicateRate * 100)
	title := fmt.Sprintf("%d%% of traffic is exact-duplicate requests", pct)
	summary := fmt.Sprintf(
		"%d%% of requests (%d of %d, over the last %d days) exactly repeat an earlier request and could be served from cache.",
		pct, duplicates, total, repeatedRequestWindowDays,
	)

	return []Candidate{{
		Kind: KindRepeatedRequest, Fingerprint: fingerprint(appID, KindRepeatedRequest, "", ""),
		Title: title, Summary: summary,
		WindowStart: windowStart, WindowEnd: windowEnd, SampleRequests: total,
		CurrentCostMicro: currentCostMonthly, ProjectedCostMicro: projectedCostMonthly,
		SavingsMicro: bestSavingsMonthly, SavingsPct: savingsPct,
		Confidence: confidence, ConfidenceScore: confidence.Score(),
		Evidence: evidence, Recommendation: recommendation,
		DetectorVersion: RepeatedRequestDetectorVersion,
	}}
}

// repeatedRequestConfidence mirrors the Model Cost detector's tiering
// style (Part G.3.1 doesn't literally apply to this kind, but nothing
// in G.3.2 defines its own bands either — this keeps every detector's
// confidence story consistent rather than inventing a one-off scheme).
func repeatedRequestConfidence(duplicateRate float64, sample int64) Confidence {
	switch {
	case duplicateRate >= 0.15 && sample >= 5000:
		return ConfidenceHigh
	case duplicateRate >= 0.08 && sample >= 1000:
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}

// topDuplicateShapes groups requests that belong to a repeated cache key
// by (system_prompt_hash, model) — never raw prompt text (Part G.3.2) —
// and returns the top 5 by count.
func topDuplicateShapes(facts []RepeatedRequestFact) []DuplicateShape {
	counts := make(map[string]int, len(facts))
	for _, f := range facts {
		counts[string(f.CacheKey)]++
	}

	type shapeKey struct{ hash, model string }
	grouped := make(map[shapeKey][]int)
	for _, f := range facts {
		if counts[string(f.CacheKey)] < 2 {
			continue
		}
		k := shapeKey{hash: hex.EncodeToString(f.SystemPromptHash), model: f.RequestedModel}
		grouped[k] = append(grouped[k], f.InputTokens)
	}

	shapes := make([]DuplicateShape, 0, len(grouped))
	for k, toks := range grouped {
		shapes = append(shapes, DuplicateShape{
			SystemPromptHash: k.hash, Model: k.model, InputTokensP50: percentile(toks, 0.5), Count: int64(len(toks)),
		})
	}
	sort.Slice(shapes, func(i, j int) bool {
		if shapes[i].Count != shapes[j].Count {
			return shapes[i].Count > shapes[j].Count
		}
		// Stable, deterministic tiebreak for equal counts.
		return shapes[i].SystemPromptHash+shapes[i].Model < shapes[j].SystemPromptHash+shapes[j].Model
	})
	if len(shapes) > 5 {
		shapes = shapes[:5]
	}
	return shapes
}
