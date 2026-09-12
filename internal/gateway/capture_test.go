package gateway

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"fluxen/internal/auth"
	"fluxen/internal/ingest"
	"fluxen/internal/retention"
	"fluxen/internal/store"
	"fluxen/pkg/providers"
	"fluxen/pkg/types"
)

// TestGateway_BodyCapture_OnlyWhenOrgEnabledIt is a real, DB-backed test
// of Part G.6's opt-in body capture (Settings > Retention, off by
// default): a real org, a real retention.Snapshot backed by real
// Postgres, and an assertion that the emitted UsageRecord actually
// carries the request/response bytes only when the org's own
// body_capture_enabled column is true — and never otherwise, which was
// the exact toggle that previously saved to the database and did
// nothing.
func TestGateway_BodyCapture_OnlyWhenOrgEnabledIt(t *testing.T) {
	pool := newTestPool(t)

	orgs := store.NewOrganizations(pool)
	apps := store.NewApplications(pool)
	keys := store.NewAPIKeys(pool)

	org, err := orgs.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("failed to seed org: %v", err)
	}
	app, err := apps.Create(context.Background(), org.ID, "app", "App")
	if err != nil {
		t.Fatalf("failed to seed app: %v", err)
	}
	rawKey, _, err := keys.Create(context.Background(), app.ID, "test")
	if err != nil {
		t.Fatalf("failed to issue key: %v", err)
	}

	provider := &fakeProvider{
		chatFunc: func(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (*types.CanonicalResponse, error) {
			raw := []byte(`{"id":"chatcmpl-1","model":"gpt-4o-mini","choices":[{"message":{"role":"assistant","content":"hi"}}]}`)
			return &types.CanonicalResponse{ID: "chatcmpl-1", Model: "gpt-4o-mini", Raw: raw}, nil
		},
	}

	snapshot := retention.NewSnapshot(orgs)
	s := NewServer(Server{
		Resolver: auth.NewResolver(keys), Provider: provider, Credential: providers.Credential{APIKey: "sk-test"},
		Catalog: testCatalog(t), Queue: ingest.NewQueue(100, nil), RetentionSettings: snapshot,
	})
	s.Timeout = 5 * time.Second

	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	// Capture is off by default (the store's own column default) — the
	// first request must never carry a body.
	resp1 := chatRequest(t, ts, rawKey, "gpt-4o-mini")
	resp1.Body.Close()
	rec1 := drainOne(t, s.Queue)
	if rec1.RequestBody != nil || rec1.ResponseBody != nil {
		t.Errorf("expected no captured body while capture is disabled, got request=%q response=%q", rec1.RequestBody, rec1.ResponseBody)
	}

	// Turn capture on for the org. retention.Snapshot has no pub/sub
	// invalidation (unlike policy/credentials) — a documented, deliberate
	// tradeoff for a low-frequency setting — so wait out its 5s TTL
	// before the next request, the same real-world lag an operator would
	// see after flipping the toggle.
	if err := orgs.UpdateRetentionSettings(context.Background(), org.ID, store.RetentionSettings{
		RequestsRetentionDays: 90, BodyRetentionDays: 7, BodyCaptureEnabled: true,
	}); err != nil {
		t.Fatalf("failed to enable capture: %v", err)
	}
	time.Sleep(6 * time.Second)

	resp2 := chatRequest(t, ts, rawKey, "gpt-4o-mini")
	resp2.Body.Close()
	rec2 := drainOne(t, s.Queue)
	if len(rec2.RequestBody) == 0 {
		t.Error("expected the request body to be captured once the org enabled it")
	}
	if len(rec2.ResponseBody) == 0 {
		t.Error("expected the response body to be captured once the org enabled it")
	}
}
