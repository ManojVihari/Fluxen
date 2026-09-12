package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRequests_ListAndGetDetail(t *testing.T) {
	_, client, baseURL, pool := newTestEnv(t)
	app := signUpAndCreateApp(t, client, baseURL, "Document AI")

	orgResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/auth/session", nil)
	var session sessionResponse
	decodeJSON(t, orgResp, &session)

	id := uuid.NewString()
	startedAt := time.Now().UTC().Add(-time.Hour)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO requests (
			id, org_id, app_id, started_at, duration_ms,
			endpoint, protocol, streamed,
			requested_model, provider, model,
			input_tokens, output_tokens, total_tokens,
			cost_micro, cost_status, cache_status, status, http_status
		) VALUES ($1, $2, $3, $4, 250, 'chat.completions', 'openai', false, 'gpt-4o-mini', 'openai', 'gpt-4o-mini', 20, 10, 30, 2000, 'known', 'miss', 'ok', 200)
	`, id, session.OrgID, app.ID, startedAt)
	if err != nil {
		t.Fatalf("unexpected error seeding request: %v", err)
	}

	listResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/requests?app_id="+app.ID, nil)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", listResp.StatusCode)
	}
	var listOut struct {
		Requests   []requestRowResponse `json:"requests"`
		NextCursor string               `json:"next_cursor"`
	}
	decodeJSON(t, listResp, &listOut)
	if len(listOut.Requests) != 1 || listOut.Requests[0].ID != id {
		t.Fatalf("expected the seeded request in the list, got %+v", listOut.Requests)
	}
	if listOut.NextCursor != "" {
		t.Errorf("expected no next cursor for a single-row result, got %q", listOut.NextCursor)
	}

	detailResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/requests/"+id, nil)
	if detailResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", detailResp.StatusCode)
	}
	var detail requestDetailResponse
	decodeJSON(t, detailResp, &detail)
	if detail.Model != "gpt-4o-mini" || detail.CostMicro != 2000 {
		t.Errorf("unexpected detail: %+v", detail)
	}

	notFoundResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/requests/"+uuid.NewString(), nil)
	if notFoundResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown request id, got %d", notFoundResp.StatusCode)
	}
	notFoundResp.Body.Close()
}

func TestRequests_UnknownApplicationFilterIs404(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	signUpAndCreateApp(t, client, baseURL, "Document AI")

	resp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/requests?app_id="+uuid.NewString(), nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 filtering by an application the org doesn't own, got %d", resp.StatusCode)
	}
}
