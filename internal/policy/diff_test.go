package policy

import (
	"encoding/json"
	"testing"

	corepolicy "fluxen/pkg/policy"
)

func TestDiff_NilToNilIsEmpty(t *testing.T) {
	d, err := Diff(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(d, &m); err != nil {
		t.Fatalf("unexpected error unmarshaling: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("expected an empty diff for nil -> nil, got %s", d)
	}
}

func TestDiff_OnlyChangedFieldsAppear(t *testing.T) {
	old := &corepolicy.PolicyDocument{
		RateLimit: &corepolicy.RateLimitPolicy{Enabled: true, RequestsPerMinute: 60},
	}
	updated := &corepolicy.PolicyDocument{
		RateLimit: &corepolicy.RateLimitPolicy{Enabled: true, RequestsPerMinute: 60},                        // unchanged
		Budget:    &corepolicy.BudgetPolicy{Enabled: true, Period: "daily", Mode: "hard", LimitMicro: 1000}, // new
	}

	d, err := Diff(old, updated)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(d, &m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := m["rate_limit"]; ok {
		t.Error("expected an unchanged field to not appear in the diff")
	}
	if _, ok := m["budget"]; !ok {
		t.Error("expected the newly-set budget field to appear in the diff")
	}
}

func TestDiff_NilDocumentTreatedAsEmpty(t *testing.T) {
	updated := &corepolicy.PolicyDocument{
		Routing: &corepolicy.RoutingPolicy{Enabled: true, FromModel: "gpt-4o", ToModel: "gpt-4o-mini", Weight: 0.5},
	}
	d, err := Diff(nil, updated)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]fieldDiff
	if err := json.Unmarshal(d, &m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entry, ok := m["routing"]
	if !ok {
		t.Fatal("expected routing to appear in the diff")
	}
	if entry.Old != nil {
		t.Errorf("expected old value to be nil for a first-time policy, got %v", entry.Old)
	}
}
