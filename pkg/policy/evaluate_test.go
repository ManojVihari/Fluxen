package policy

import (
	"math/rand"
	"testing"
)

func TestEvaluate_NilDocumentIsDirect(t *testing.T) {
	d := Evaluate(nil, "gpt-4o", "key", nil)
	if d.Model != "gpt-4o" || d.RouteReason != "direct" || d.Blocked {
		t.Errorf("expected a nil document to route direct and unblocked, got %+v", d)
	}
}

func TestEvaluate_ModelRestriction_BlocksDisallowedModel(t *testing.T) {
	doc := &PolicyDocument{ModelRestriction: &ModelRestrictionPolicy{Enabled: true, AllowedModels: []string{"gpt-4o-mini"}}}

	d := Evaluate(doc, "gpt-4o", "key", nil)
	if !d.Blocked || d.BlockCode != "model_not_allowed" {
		t.Fatalf("expected gpt-4o to be blocked, got %+v", d)
	}

	d2 := Evaluate(doc, "gpt-4o-mini", "key", nil)
	if d2.Blocked {
		t.Fatalf("expected gpt-4o-mini (allowed) to pass through, got %+v", d2)
	}
}

func TestEvaluate_ModelRestriction_DisabledNeverBlocks(t *testing.T) {
	doc := &PolicyDocument{ModelRestriction: &ModelRestrictionPolicy{Enabled: false, AllowedModels: []string{"gpt-4o-mini"}}}
	d := Evaluate(doc, "gpt-4o", "key", nil)
	if d.Blocked {
		t.Errorf("expected a disabled restriction to never block, got %+v", d)
	}
}

func TestEvaluate_Routing_RandomSplitConvergesToWeight(t *testing.T) {
	doc := &PolicyDocument{Routing: &RoutingPolicy{Enabled: true, FromModel: "gpt-4o", ToModel: "gpt-4o-mini", Weight: 0.3}}
	rng := rand.New(rand.NewSource(1))

	const n = 100_000
	var routedToCandidate int
	for i := 0; i < n; i++ {
		d := Evaluate(doc, "gpt-4o", "", rng)
		if d.RouteReason != "split" {
			t.Fatalf("expected route reason 'split', got %q", d.RouteReason)
		}
		if d.Model == "gpt-4o-mini" {
			routedToCandidate++
			if d.RouteVariant != "B" {
				t.Fatalf("expected variant B when routed to candidate, got %q", d.RouteVariant)
			}
		} else if d.RouteVariant != "A" {
			t.Fatalf("expected variant A when not routed, got %q", d.RouteVariant)
		}
	}

	fraction := float64(routedToCandidate) / n
	if fraction < 0.29 || fraction > 0.31 {
		t.Errorf("expected routed fraction to converge to ~0.3, got %v", fraction)
	}
}

func TestEvaluate_Routing_StickyKeyAlwaysPicksSameVariant(t *testing.T) {
	doc := &PolicyDocument{Routing: &RoutingPolicy{Enabled: true, FromModel: "gpt-4o", ToModel: "gpt-4o-mini", Weight: 0.5, Sticky: true}}

	first := Evaluate(doc, "gpt-4o", "user-42", nil)
	for i := 0; i < 20; i++ {
		again := Evaluate(doc, "gpt-4o", "user-42", nil)
		if again.Model != first.Model || again.RouteVariant != first.RouteVariant {
			t.Fatalf("expected sticky routing to always pick the same variant for the same key, got %+v then %+v", first, again)
		}
	}
}

func TestEvaluate_Routing_OnlyAppliesToFromModel(t *testing.T) {
	doc := &PolicyDocument{Routing: &RoutingPolicy{Enabled: true, FromModel: "gpt-4o", ToModel: "gpt-4o-mini", Weight: 1.0}}
	d := Evaluate(doc, "gpt-3.5-turbo", "key", rand.New(rand.NewSource(1)))
	if d.RouteReason != "direct" || d.Model != "gpt-3.5-turbo" {
		t.Errorf("expected a non-matching requested model to pass through untouched, got %+v", d)
	}
}

func TestEvaluate_Routing_DisabledPassesThrough(t *testing.T) {
	doc := &PolicyDocument{Routing: &RoutingPolicy{Enabled: false, FromModel: "gpt-4o", ToModel: "gpt-4o-mini", Weight: 1.0}}
	d := Evaluate(doc, "gpt-4o", "key", rand.New(rand.NewSource(1)))
	if d.RouteReason != "direct" {
		t.Errorf("expected a disabled routing rule to never reroute, got %+v", d)
	}
}

func TestEvaluateBudget(t *testing.T) {
	cases := []struct {
		name       string
		doc        *BudgetPolicy
		spentMicro int64
		want       bool
	}{
		{"nil doc never blocks", nil, 1_000_000, false},
		{"disabled never blocks", &BudgetPolicy{Enabled: false, Mode: BudgetModeHard, LimitMicro: 100}, 1_000_000, false},
		{"soft mode never blocks", &BudgetPolicy{Enabled: true, Mode: BudgetModeSoft, LimitMicro: 100}, 1_000_000, false},
		{"hard mode under limit allows", &BudgetPolicy{Enabled: true, Mode: BudgetModeHard, LimitMicro: 1000}, 999, false},
		{"hard mode at limit blocks", &BudgetPolicy{Enabled: true, Mode: BudgetModeHard, LimitMicro: 1000}, 1000, true},
		{"hard mode over limit blocks", &BudgetPolicy{Enabled: true, Mode: BudgetModeHard, LimitMicro: 1000}, 1001, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EvaluateBudget(tc.doc, tc.spentMicro); got != tc.want {
				t.Errorf("EvaluateBudget(%+v, %d) = %v, want %v", tc.doc, tc.spentMicro, got, tc.want)
			}
		})
	}
}
