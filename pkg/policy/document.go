// Package policy defines the PolicyDocument schema and the pure
// Evaluate/EvaluateBudget functions the gateway (production) and
// internal/sim (simulation) both call — Rule 9's purity invariant (Part
// C.2): no I/O, no wall-clock reads except an injected time, no
// randomness except an injected source. The five named controls (Part
// PRD §22) are: model routing, exact caching, budget, rate limit, and
// model restriction — caching's enforcement (internal/cache) and rate
// limiting's live counter (internal/guard) are inherently stateful/I/O
// and live outside this package; only their policy fields and (for
// budget) the pure over-limit decision belong here.
package policy

import "fmt"

// PolicyDocument is the versioned, per-application control surface (Part
// E.1's policies.document). Every field is optional and nil/disabled by
// default — an application with no policy document behaves exactly like
// Phase 1's pipeline: every request goes straight to its requested
// model, uncached, unmetered, unrestricted.
type PolicyDocument struct {
	Routing          *RoutingPolicy          `json:"routing,omitempty"`
	Caching          *CachingPolicy          `json:"caching,omitempty"`
	Budget           *BudgetPolicy           `json:"budget,omitempty"`
	RateLimit        *RateLimitPolicy        `json:"rate_limit,omitempty"`
	ModelRestriction *ModelRestrictionPolicy `json:"model_restriction,omitempty"`
}

// RoutingPolicy is percentage-based model routing (PRD §22): reroute a
// share of one model's traffic to another — the control the Model Cost
// opportunity's recommendation (Part G.3.1) applies.
type RoutingPolicy struct {
	Enabled bool `json:"enabled"`
	// FromModel is the requested model this rule applies to; a request
	// for any other model passes through untouched.
	FromModel string `json:"from_model"`
	ToModel   string `json:"to_model"`
	// Weight is the fraction of FromModel's traffic routed to ToModel,
	// in [0, 1].
	Weight float64 `json:"weight"`
	// Sticky, when true, routes the same sticky key (Part C.5:
	// "sticky-by-session") to the same variant every time instead of
	// per-request randomness — useful when a caller's own retries or a
	// multi-turn conversation should never split across two models
	// mid-flight.
	Sticky bool `json:"sticky"`
}

// CachingPolicy is exact-match response caching (PRD §22): enable/disable
// only — the key function, eligibility, and storage are internal/cache's
// job, not a policy field.
type CachingPolicy struct {
	Enabled    bool `json:"enabled"`
	TTLSeconds int  `json:"ttl_seconds"`
}

// BudgetPolicy is an application spending limit (PRD §22). Mode "hard"
// blocks new requests once the period limit is reached; "soft" never
// blocks — the counter still runs (visible in usage), it just never
// enforces (a warn-only mode operators can dial back to at will).
type BudgetPolicy struct {
	Enabled bool `json:"enabled"`
	// Period is "daily" or "monthly" — both reset at UTC boundaries
	// (Part C.5's guard: a period counter, not a rolling window).
	Period     string `json:"period"`
	LimitMicro int64  `json:"limit_micro"`
	Mode       string `json:"mode"`
}

// RateLimitPolicy is an application request-rate limit (PRD §22).
type RateLimitPolicy struct {
	Enabled           bool `json:"enabled"`
	RequestsPerMinute int  `json:"requests_per_minute"`
}

// ModelRestrictionPolicy restricts an application to an approved model
// list (PRD §22).
type ModelRestrictionPolicy struct {
	Enabled       bool     `json:"enabled"`
	AllowedModels []string `json:"allowed_models"`
}

const (
	BudgetPeriodDaily   = "daily"
	BudgetPeriodMonthly = "monthly"
	BudgetModeHard      = "hard"
	BudgetModeSoft      = "soft"
)

// Validate checks a document's internal consistency before it's ever
// written (Part L Phase 5: "validate the target PolicyDocument" is the
// apply transaction's first step). A nil document is always valid — it's
// the no-op policy.
func (d *PolicyDocument) Validate() error {
	if d == nil {
		return nil
	}
	if d.Routing != nil && d.Routing.Enabled {
		if d.Routing.FromModel == "" || d.Routing.ToModel == "" {
			return fmt.Errorf("policy: routing requires both from_model and to_model")
		}
		if d.Routing.FromModel == d.Routing.ToModel {
			return fmt.Errorf("policy: routing from_model and to_model must differ")
		}
		if d.Routing.Weight < 0 || d.Routing.Weight > 1 {
			return fmt.Errorf("policy: routing weight must be in [0, 1], got %v", d.Routing.Weight)
		}
	}
	if d.Caching != nil && d.Caching.Enabled && d.Caching.TTLSeconds <= 0 {
		return fmt.Errorf("policy: caching requires a positive ttl_seconds")
	}
	if d.Budget != nil && d.Budget.Enabled {
		if d.Budget.Period != BudgetPeriodDaily && d.Budget.Period != BudgetPeriodMonthly {
			return fmt.Errorf("policy: budget period must be %q or %q, got %q", BudgetPeriodDaily, BudgetPeriodMonthly, d.Budget.Period)
		}
		if d.Budget.Mode != BudgetModeHard && d.Budget.Mode != BudgetModeSoft {
			return fmt.Errorf("policy: budget mode must be %q or %q, got %q", BudgetModeHard, BudgetModeSoft, d.Budget.Mode)
		}
		if d.Budget.LimitMicro <= 0 {
			return fmt.Errorf("policy: budget requires a positive limit_micro")
		}
	}
	if d.RateLimit != nil && d.RateLimit.Enabled && d.RateLimit.RequestsPerMinute <= 0 {
		return fmt.Errorf("policy: rate_limit requires a positive requests_per_minute")
	}
	if d.ModelRestriction != nil && d.ModelRestriction.Enabled && len(d.ModelRestriction.AllowedModels) == 0 {
		return fmt.Errorf("policy: model_restriction requires at least one allowed model")
	}
	return nil
}
