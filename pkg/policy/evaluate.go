package policy

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand"
)

// Decision is Evaluate's output: which model to actually call, why, and
// whether the request is blocked outright. The gateway's Account stage
// records RouteReason/RouteVariant on every request verbatim (Part E.1:
// requests.route_reason/route_variant), so a request's own row always
// explains which policy path it took.
type Decision struct {
	// Model is the model to actually call — equal to the requested model
	// unless routing rerouted it.
	Model        string
	RouteReason  string // "direct" | "split" | "restriction"
	RouteVariant string // "" | "A" | "B" — only set when RouteReason is "split"

	Blocked   bool
	BlockCode string // "model_not_allowed" when Blocked
}

// Evaluate is the pure policy decision function both the gateway
// (production) and internal/sim (simulation) call (Part G.4/Rule 9) —
// model restriction, then percentage routing. Budget and rate-limit
// enforcement are stateful (they need a live spend/request counter) and
// live in internal/guard; EvaluateBudget below is their pure sub-decision.
//
// rng is required only when doc.Routing.Sticky is false — sticky routing
// is fully deterministic from stickyKey and needs no randomness, so a nil
// rng is safe to pass when every enabled routing rule is sticky.
func Evaluate(doc *PolicyDocument, requestedModel, stickyKey string, rng *rand.Rand) Decision {
	if doc == nil {
		return Decision{Model: requestedModel, RouteReason: "direct"}
	}

	if r := doc.ModelRestriction; r != nil && r.Enabled && !contains(r.AllowedModels, requestedModel) {
		return Decision{Model: requestedModel, RouteReason: "restriction", Blocked: true, BlockCode: "model_not_allowed"}
	}

	if r := doc.Routing; r != nil && r.Enabled && requestedModel == r.FromModel {
		routeToCandidate := false
		if r.Sticky && stickyKey != "" {
			routeToCandidate = stickyFraction(stickyKey) < r.Weight
		} else if rng != nil {
			routeToCandidate = rng.Float64() < r.Weight
		}
		if routeToCandidate {
			return Decision{Model: r.ToModel, RouteReason: "split", RouteVariant: "B"}
		}
		return Decision{Model: requestedModel, RouteReason: "split", RouteVariant: "A"}
	}

	return Decision{Model: requestedModel, RouteReason: "direct"}
}

// stickyFraction deterministically maps a sticky key to a value in
// [0, 1) — the same key always lands on the same side of a given weight,
// so a sticky routing rule never splits one caller's own traffic across
// both variants.
func stickyFraction(key string) float64 {
	sum := sha256.Sum256([]byte(key))
	n := binary.BigEndian.Uint64(sum[:8])
	return float64(n) / float64(1<<64)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// EvaluateBudget is the pure over-limit decision both internal/guard
// (live gateway, real Redis-backed spend counter) and internal/sim's
// budget-impact scenario (a replay accumulator) call. V1 checks only
// spend already recorded before this request — it does not try to
// predict this request's own cost ahead of calling the provider (V1 has
// no pre-call cost estimator), so a request that pushes spend over the
// limit is itself allowed; the next one is blocked.
func EvaluateBudget(doc *BudgetPolicy, spentMicro int64) (blocked bool) {
	if doc == nil || !doc.Enabled || doc.Mode != BudgetModeHard {
		return false
	}
	return spentMicro >= doc.LimitMicro
}
