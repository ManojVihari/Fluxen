// Package policy is the stateful half of Fluxen's control surface: the
// Postgres-backed store and history (Part E.1), the apply transaction
// (Part L Phase 5), and an in-process snapshot cache with Redis pub/sub
// invalidation so the gateway's hot path never hits Postgres per
// request. The pure decision logic (the actual schema, Evaluate,
// EvaluateBudget) lives in pkg/policy — this package reads/writes/caches
// documents, it never evaluates them.
package policy

import (
	"encoding/json"

	corepolicy "fluxen/pkg/policy"
)

type fieldDiff struct {
	Old any `json:"old"`
	New any `json:"new"`
}

// Diff computes a field-level diff between two policy documents — Part
// I.4's "diff before save" and Part E.1's policy_history.diff both need
// exactly this: which of the five controls changed, old value next to
// new. A nil document is treated as the empty (no-op) document, so
// applying a policy for the first time still produces a diff of
// "nothing -> something" for each newly-set control.
func Diff(old, updated *corepolicy.PolicyDocument) (json.RawMessage, error) {
	oldFields := fieldsOf(old)
	newFields := fieldsOf(updated)

	out := make(map[string]fieldDiff, len(newFields))
	for name, newVal := range newFields {
		oldVal := oldFields[name]
		oldJSON, err := json.Marshal(oldVal)
		if err != nil {
			return nil, err
		}
		newJSON, err := json.Marshal(newVal)
		if err != nil {
			return nil, err
		}
		if string(oldJSON) != string(newJSON) {
			out[name] = fieldDiff{Old: oldVal, New: newVal}
		}
	}
	return json.Marshal(out)
}

func fieldsOf(d *corepolicy.PolicyDocument) map[string]any {
	if d == nil {
		d = &corepolicy.PolicyDocument{}
	}
	return map[string]any{
		"routing":           d.Routing,
		"caching":           d.Caching,
		"budget":            d.Budget,
		"rate_limit":        d.RateLimit,
		"model_restriction": d.ModelRestriction,
	}
}
