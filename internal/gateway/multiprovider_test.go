package gateway

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"fluxen/internal/auth"
	"fluxen/internal/ingest"
	"fluxen/pkg/providers"
	"fluxen/pkg/types"
)

// newMultiProviderTestServer mirrors newTestServer but additionally wires
// an Ollama binding, to exercise Phase 7's provider-resolution path
// (Part A.3: "routing to OpenAI/Gemini/Ollama" from one ingress).
func newMultiProviderTestServer(t *testing.T, openaiProvider, ollamaProvider providers.Provider) (*Server, string) {
	t.Helper()

	raw, prefix, hash, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error generating key: %v", err)
	}
	lookup := &fakeKeyLookup{records: map[string]auth.APIKeyRecord{
		prefix: {ID: "key1", AppID: "app1", OrgID: "org1", KeyHash: hash},
	}}
	resolver := auth.NewResolver(lookup)

	s := NewServer(Server{
		Resolver: resolver, Provider: openaiProvider, Credential: providers.Credential{APIKey: "sk-test"},
		Providers:   map[string]providers.Provider{"ollama": ollamaProvider},
		Credentials: map[string]providers.Credential{"ollama": {BaseURL: "http://ollama.local"}},
		Catalog:     testCatalog(t), Queue: ingest.NewQueue(100, nil),
	})
	s.Timeout = 5 * time.Second
	return s, raw
}

// TestGateway_UncatalogedModel_RoutesToOllamaAsCostLocal is the
// Phase 7 multi-provider test: a model absent from the pricing catalog,
// with an Ollama binding configured, must be served by Ollama and
// accounted as CostStatus=CostLocal/$0 (Part D.1, frozen decision) —
// never CostUnknown, and never routed to the OpenAI binding.
func TestGateway_UncatalogedModel_RoutesToOllamaAsCostLocal(t *testing.T) {
	openaiCalled := false
	openaiProvider := &fakeProvider{
		chatFunc: func(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (*types.CanonicalResponse, error) {
			openaiCalled = true
			return nil, fmt.Errorf("openai should never be called in this test")
		},
	}
	ollamaProvider := &fakeProvider{
		chatFunc: func(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (*types.CanonicalResponse, error) {
			if cred.BaseURL != "http://ollama.local" {
				t.Errorf("expected the ollama credential to be passed through, got %+v", cred)
			}
			raw := []byte(`{"id":"chatcmpl-1","model":"llama3.2","choices":[{"message":{"role":"assistant","content":"hi"}}]}`)
			return &types.CanonicalResponse{
				ID: "chatcmpl-1", Model: "llama3.2", Raw: raw,
				Usage: types.ResponseUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			}, nil
		},
	}

	s, rawKey := newMultiProviderTestServer(t, openaiProvider, ollamaProvider)
	queue := s.Queue

	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	body := `{"model":"llama3.2","messages":[{"role":"user","content":"hi"}]}`
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
	if openaiCalled {
		t.Error("expected the OpenAI provider to never be called for an uncataloged model with Ollama configured")
	}

	rec := drainOne(t, queue)
	if rec.Provider != "ollama" {
		t.Errorf("expected provider=ollama, got %q", rec.Provider)
	}
	if rec.CostStatus != types.CostLocal {
		t.Errorf("expected CostStatus=local, got %q", rec.CostStatus)
	}
	if rec.Cost != 0 {
		t.Errorf("expected zero cost for a local Ollama request, got %d", rec.Cost)
	}
}

// TestGateway_CatalogedModel_StillRoutesToOpenAI confirms a known,
// catalog-priced model keeps routing to OpenAI even when an Ollama
// binding is also configured -- the catalog is checked first.
func TestGateway_CatalogedModel_StillRoutesToOpenAI(t *testing.T) {
	ollamaCalled := false
	openaiProvider := &fakeProvider{
		chatFunc: func(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (*types.CanonicalResponse, error) {
			raw := []byte(`{"id":"chatcmpl-1","model":"gpt-4o-mini","choices":[{"message":{"role":"assistant","content":"hi"}}]}`)
			return &types.CanonicalResponse{
				ID: "chatcmpl-1", Model: "gpt-4o-mini", Raw: raw,
				Usage: types.ResponseUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			}, nil
		},
	}
	ollamaProvider := &fakeProvider{
		chatFunc: func(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (*types.CanonicalResponse, error) {
			ollamaCalled = true
			return nil, fmt.Errorf("ollama should never be called in this test")
		},
	}

	s, rawKey := newMultiProviderTestServer(t, openaiProvider, ollamaProvider)
	queue := s.Queue

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
	if ollamaCalled {
		t.Error("expected a catalog-priced model to never route to Ollama")
	}

	rec := drainOne(t, queue)
	if rec.Provider != "openai" {
		t.Errorf("expected provider=openai, got %q", rec.Provider)
	}
	if rec.CostStatus != types.CostKnown {
		t.Errorf("expected CostStatus=known, got %q", rec.CostStatus)
	}
}

// TestGateway_UncatalogedModel_NeverRoutesToOllamaWithoutACredential is a
// regression test: an Ollama *client instance* existing in Server.Providers
// (constructed unconditionally at boot, cred-independent) must never by
// itself be read as "Ollama is configured." Without any credential for
// Ollama from either the static map or the snapshot, an uncataloged model
// must fall through to the OpenAI binding, not silently attempt a call
// against a completely unconfigured Ollama.
func TestGateway_UncatalogedModel_NeverRoutesToOllamaWithoutACredential(t *testing.T) {
	openaiCalled := false
	ollamaCalled := false
	openaiProvider := &fakeProvider{
		chatFunc: func(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (*types.CanonicalResponse, error) {
			openaiCalled = true
			raw := []byte(`{"id":"chatcmpl-1","model":"llama3.2","choices":[{"message":{"role":"assistant","content":"hi"}}]}`)
			return &types.CanonicalResponse{ID: "chatcmpl-1", Model: "llama3.2", Raw: raw}, nil
		},
	}
	ollamaProvider := &fakeProvider{
		chatFunc: func(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (*types.CanonicalResponse, error) {
			ollamaCalled = true
			return nil, fmt.Errorf("ollama should never be called: no credential is configured for it")
		},
	}

	raw, prefix, hash, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error generating key: %v", err)
	}
	lookup := &fakeKeyLookup{records: map[string]auth.APIKeyRecord{
		prefix: {ID: "key1", AppID: "app1", OrgID: "org1", KeyHash: hash},
	}}
	resolver := auth.NewResolver(lookup)

	// Providers holds a live Ollama client instance (as main.go always
	// constructs one now) but Credentials carries no entry for it at
	// all — the exact scenario a boot-time-only client existence check
	// would have gotten wrong.
	s := NewServer(Server{
		Resolver: resolver, Provider: openaiProvider, Credential: providers.Credential{APIKey: "sk-test"},
		Providers: map[string]providers.Provider{"ollama": ollamaProvider},
		Catalog:   testCatalog(t), Queue: ingest.NewQueue(100, nil),
	})
	s.Timeout = 5 * time.Second

	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	body := `{"model":"llama3.2","messages":[{"role":"user","content":"hi"}]}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+raw)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if ollamaCalled {
		t.Fatal("expected Ollama to never be called with no credential configured for it")
	}
	if !openaiCalled {
		t.Error("expected the request to fall through to the OpenAI binding instead")
	}
}
