package policy

import "testing"

func TestValidate_NilIsAlwaysValid(t *testing.T) {
	var d *PolicyDocument
	if err := d.Validate(); err != nil {
		t.Errorf("expected nil document to be valid, got %v", err)
	}
}

func TestValidate_EmptyDocumentIsValid(t *testing.T) {
	if err := (&PolicyDocument{}).Validate(); err != nil {
		t.Errorf("expected an empty document to be valid, got %v", err)
	}
}

func TestValidate_RoutingRequiresModelsAndValidWeight(t *testing.T) {
	cases := []struct {
		name    string
		routing RoutingPolicy
		wantErr bool
	}{
		{"valid", RoutingPolicy{Enabled: true, FromModel: "gpt-4o", ToModel: "gpt-4o-mini", Weight: 0.5}, false},
		{"missing from_model", RoutingPolicy{Enabled: true, ToModel: "gpt-4o-mini", Weight: 0.5}, true},
		{"missing to_model", RoutingPolicy{Enabled: true, FromModel: "gpt-4o", Weight: 0.5}, true},
		{"same model", RoutingPolicy{Enabled: true, FromModel: "gpt-4o", ToModel: "gpt-4o", Weight: 0.5}, true},
		{"weight too high", RoutingPolicy{Enabled: true, FromModel: "gpt-4o", ToModel: "gpt-4o-mini", Weight: 1.5}, true},
		{"weight negative", RoutingPolicy{Enabled: true, FromModel: "gpt-4o", ToModel: "gpt-4o-mini", Weight: -0.1}, true},
		{"disabled skips validation", RoutingPolicy{Enabled: false, Weight: 5}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := (&PolicyDocument{Routing: &tc.routing}).Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestValidate_BudgetRequiresKnownPeriodAndMode(t *testing.T) {
	cases := []struct {
		name    string
		budget  BudgetPolicy
		wantErr bool
	}{
		{"valid hard", BudgetPolicy{Enabled: true, Period: BudgetPeriodDaily, Mode: BudgetModeHard, LimitMicro: 1000}, false},
		{"valid soft monthly", BudgetPolicy{Enabled: true, Period: BudgetPeriodMonthly, Mode: BudgetModeSoft, LimitMicro: 1000}, false},
		{"bad period", BudgetPolicy{Enabled: true, Period: "weekly", Mode: BudgetModeHard, LimitMicro: 1000}, true},
		{"bad mode", BudgetPolicy{Enabled: true, Period: BudgetPeriodDaily, Mode: "medium", LimitMicro: 1000}, true},
		{"zero limit", BudgetPolicy{Enabled: true, Period: BudgetPeriodDaily, Mode: BudgetModeHard, LimitMicro: 0}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := (&PolicyDocument{Budget: &tc.budget}).Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestValidate_RateLimitRequiresPositiveRPM(t *testing.T) {
	if err := (&PolicyDocument{RateLimit: &RateLimitPolicy{Enabled: true, RequestsPerMinute: 0}}).Validate(); err == nil {
		t.Error("expected an error for a zero requests_per_minute")
	}
	if err := (&PolicyDocument{RateLimit: &RateLimitPolicy{Enabled: true, RequestsPerMinute: 60}}).Validate(); err != nil {
		t.Errorf("expected a positive requests_per_minute to be valid, got %v", err)
	}
}

func TestValidate_ModelRestrictionRequiresAtLeastOneModel(t *testing.T) {
	if err := (&PolicyDocument{ModelRestriction: &ModelRestrictionPolicy{Enabled: true}}).Validate(); err == nil {
		t.Error("expected an error for an empty allowed-models list")
	}
	if err := (&PolicyDocument{ModelRestriction: &ModelRestrictionPolicy{Enabled: true, AllowedModels: []string{"gpt-4o-mini"}}}).Validate(); err != nil {
		t.Errorf("expected a non-empty allowed-models list to be valid, got %v", err)
	}
}

func TestValidate_CachingRequiresPositiveTTL(t *testing.T) {
	if err := (&PolicyDocument{Caching: &CachingPolicy{Enabled: true, TTLSeconds: 0}}).Validate(); err == nil {
		t.Error("expected an error for a zero ttl_seconds")
	}
}
