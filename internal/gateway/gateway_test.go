package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"fluxen/internal/auth"
	"fluxen/pkg/pricing"
	"fluxen/pkg/providers"
	"fluxen/pkg/types"

	"fluxen/internal/ingest"
)

// --- test doubles ---

type fakeKeyLookup struct {
	records map[string]auth.APIKeyRecord
}

func (f *fakeKeyLookup) LookupAPIKeyByPrefix(_ context.Context, prefix string) (auth.APIKeyRecord, bool, error) {
	rec, ok := f.records[prefix]
	return rec, ok, nil
}

type fakeProvider struct {
	chatFunc       func(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (*types.CanonicalResponse, error)
	chatStreamFunc func(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (providers.StreamReader, error)
}

func (f *fakeProvider) Name() string { return "openai" }
func (f *fakeProvider) Chat(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (*types.CanonicalResponse, error) {
	return f.chatFunc(ctx, req, cred)
}
func (f *fakeProvider) ChatStream(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (providers.StreamReader, error) {
	return f.chatStreamFunc(ctx, req, cred)
}
func (f *fakeProvider) Models(context.Context, providers.Credential) ([]providers.ModelInfo, error) {
	return nil, nil
}
func (f *fakeProvider) Health(context.Context, providers.Credential) error { return nil }

type fakeStreamReader struct {
	chunks [][]byte
	i      int
	usage  types.ResponseUsage
	delay  time.Duration
	closed bool
}

func (f *fakeStreamReader) Next() ([]byte, error) {
	if f.i >= len(f.chunks) {
		return nil, io.EOF
	}
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	c := f.chunks[f.i]
	f.i++
	return c, nil
}
func (f *fakeStreamReader) Usage() types.ResponseUsage { return f.usage }
func (f *fakeStreamReader) Close() error               { f.closed = true; return nil }

// --- test helpers ---

func testCatalog(t *testing.T) *pricing.Catalog {
	t.Helper()
	c, err := pricing.LoadEmbedded()
	if err != nil {
		t.Fatalf("failed to load embedded catalog: %v", err)
	}
	return c
}

func newTestServer(t *testing.T, provider providers.Provider) (*Server, *fakeKeyLookup, string) {
	t.Helper()

	raw, prefix, hash, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error generating key: %v", err)
	}

	lookup := &fakeKeyLookup{records: map[string]auth.APIKeyRecord{
		prefix: {ID: "key1", AppID: "app1", OrgID: "org1", KeyHash: hash},
	}}
	resolver := auth.NewResolver(lookup)

	s := NewServer(resolver, provider, providers.Credential{APIKey: "sk-test"}, testCatalog(t), ingest.NewQueue(100, nil), nil)
	s.Timeout = 5 * time.Second
	return s, lookup, raw
}

func drainOne(t *testing.T, q *ingest.Queue) types.UsageRecord {
	t.Helper()
	select {
	case rec := <-q.C():
		return rec
	case <-time.After(2 * time.Second):
		t.Fatal("expected a usage record to be emitted, got none")
		return types.UsageRecord{}
	}
}

// --- tests ---

func TestGateway_NonStreaming_Success(t *testing.T) {
	provider := &fakeProvider{
		chatFunc: func(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (*types.CanonicalResponse, error) {
			raw := []byte(`{"id":"chatcmpl-1","model":"gpt-4o-mini","choices":[{"message":{"role":"assistant","content":"hi"}}]}`)
			return &types.CanonicalResponse{
				ID: "chatcmpl-1", Model: "gpt-4o-mini", Raw: raw,
				Usage: types.ResponseUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			}, nil
		},
	}
	s, _, rawKey := newTestServer(t, provider)

	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+rawKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Header.Get(RequestIDHeader) == "" {
		t.Error("expected X-Fluxen-Request-Id header to be set")
	}

	respBody, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		t.Fatalf("expected valid JSON response, got: %s", respBody)
	}
	if parsed["id"] != "chatcmpl-1" {
		t.Errorf("expected the provider's response to be relayed verbatim, got %v", parsed)
	}

	rec := drainOne(t, s.Queue)
	if rec.AppID != "app1" {
		t.Errorf("expected AppID=app1, got %v", rec.AppID)
	}
	if rec.Status != "ok" {
		t.Errorf("expected status=ok, got %q", rec.Status)
	}
	if rec.InputTokens != 10 || rec.OutputTokens != 5 {
		t.Errorf("expected token counts to be captured, got in=%d out=%d", rec.InputTokens, rec.OutputTokens)
	}
	if rec.CostStatus != types.CostKnown {
		t.Errorf("expected cost_status=known for a catalog model, got %v", rec.CostStatus)
	}
	if rec.Cost == 0 {
		t.Error("expected a non-zero cost for a known, priced model with real usage")
	}
}

func TestGateway_MissingAuth_401(t *testing.T) {
	s, _, _ := newTestServer(t, &fakeProvider{})
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no Authorization header, got %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Fluxen-Error-Code") != "invalid_api_key" {
		t.Errorf("expected X-Fluxen-Error-Code=invalid_api_key, got %q", resp.Header.Get("X-Fluxen-Error-Code"))
	}
}

func TestGateway_RevokedKey_401(t *testing.T) {
	provider := &fakeProvider{}
	s, lookup, rawKey := newTestServer(t, provider)

	// Revoke the key after issuance, before the request.
	for prefix, rec := range lookup.records {
		rec.Revoked = true
		lookup.records[prefix] = rec
	}

	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4o-mini","messages":[]}`))
	req.Header.Set("Authorization", "Bearer "+rawKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for a revoked key, got %d", resp.StatusCode)
	}
}

func TestGateway_Streaming_Success(t *testing.T) {
	chunks := [][]byte{
		[]byte("data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n"),
		[]byte("data: [DONE]\n\n"),
	}
	provider := &fakeProvider{
		chatStreamFunc: func(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (providers.StreamReader, error) {
			return &fakeStreamReader{chunks: chunks, usage: types.ResponseUsage{InputTokens: 7, OutputTokens: 3, TotalTokens: 10}}, nil
		},
	}
	s, _, rawKey := newTestServer(t, provider)

	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+rawKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %q", ct)
	}

	respBody, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(respBody, []byte(`"content":"Hi"`)) {
		t.Errorf("expected the streamed content to be forwarded verbatim, got %s", respBody)
	}
	if !bytes.Contains(respBody, []byte("[DONE]")) {
		t.Errorf("expected the [DONE] event to be forwarded, got %s", respBody)
	}

	rec := drainOne(t, s.Queue)
	if rec.Status != "ok" {
		t.Errorf("expected status=ok, got %q", rec.Status)
	}
	if !rec.Streamed {
		t.Error("expected Streamed=true")
	}
	if rec.TotalTokens != 10 {
		t.Errorf("expected TotalTokens=10 from the stream's final usage, got %d", rec.TotalTokens)
	}
	if rec.TTFTMS == nil {
		t.Error("expected TTFTMS to be recorded for a streamed response")
	}
}

func TestGateway_ClientAbort_StillEmitsUsage(t *testing.T) {
	// A stream that yields several chunks with a small delay between each,
	// long enough for the test to cancel the client request partway
	// through and observe the resulting client_abort accounting.
	chunks := [][]byte{
		[]byte("data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n"),
		[]byte("data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\n"),
		[]byte("data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"c\"}}]}\n\n"),
		[]byte("data: [DONE]\n\n"),
	}
	sr := &fakeStreamReader{chunks: chunks, delay: 100 * time.Millisecond, usage: types.ResponseUsage{InputTokens: 5, OutputTokens: 1, TotalTokens: 6}}
	provider := &fakeProvider{
		chatStreamFunc: func(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (providers.StreamReader, error) {
			return sr, nil
		},
	}
	s, _, rawKey := newTestServer(t, provider)

	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+rawKey)

	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		// The client may successfully read the first chunk before the
		// context deadline fires on a subsequent read; either way, close
		// the body to force the abort if it hasn't happened yet.
		_ = resp.Body.Close()
	}

	rec := drainOne(t, s.Queue)
	if rec.Status != "client_abort" {
		t.Fatalf("expected status=client_abort after the client canceled mid-stream, got %q", rec.Status)
	}
	if !sr.closed {
		t.Error("expected the upstream stream reader to be closed after a client abort")
	}
}
