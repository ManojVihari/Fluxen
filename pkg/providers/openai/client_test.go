package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"fluxen/pkg/providers"
	"fluxen/pkg/types"
)

func TestClient_Chat_Success(t *testing.T) {
	fixture, err := os.ReadFile("testdata/chat_response.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("expected path /v1/chat/completions, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(fixture)
	}))
	defer srv.Close()

	client := NewClient(srv.Client())
	req, err := types.ParseCanonicalRequest([]byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp, err := client.Chat(context.Background(), req, providers.Credential{APIKey: "sk-test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotAuth != "Bearer sk-test" {
		t.Errorf("expected Authorization header 'Bearer sk-test', got %q", gotAuth)
	}
	if resp.Usage.InputTokens != 12 {
		t.Errorf("expected InputTokens=12, got %d", resp.Usage.InputTokens)
	}
}

func TestClient_Chat_UpstreamErrorRelayed(t *testing.T) {
	fixture, err := os.ReadFile("testdata/error_response.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write(fixture)
	}))
	defer srv.Close()

	client := NewClient(srv.Client())
	req, _ := types.ParseCanonicalRequest([]byte(`{"model":"gpt-4o-mini","messages":[]}`))

	_, err = client.Chat(context.Background(), req, providers.Credential{APIKey: "bad-key", BaseURL: srv.URL})
	if err == nil {
		t.Fatal("expected an error for a 401 upstream response")
	}

	upstreamErr, ok := err.(*providers.UpstreamError)
	if !ok {
		t.Fatalf("expected a *providers.UpstreamError, got %T: %v", err, err)
	}
	if upstreamErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", upstreamErr.StatusCode)
	}
	if string(upstreamErr.Body) != string(fixture) {
		t.Error("expected the upstream error body to be relayed verbatim")
	}
}

func TestClient_ChatStream_ForwardsChunksAndCapturesUsage(t *testing.T) {
	sse := "data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n" +
		"data: {\"id\":\"1\",\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":1,\"total_tokens\":6}}\n\n" +
		"data: [DONE]\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, sse)
	}))
	defer srv.Close()

	client := NewClient(srv.Client())
	req, _ := types.ParseCanonicalRequest([]byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}],"stream":true}`))

	sr, err := client.ChatStream(context.Background(), req, providers.Credential{APIKey: "sk-test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer sr.Close()

	n := 0
	for {
		_, err := sr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error reading stream: %v", err)
		}
		n++
	}
	if n != 3 {
		t.Fatalf("expected 3 chunks, got %d", n)
	}

	usage := sr.Usage()
	if usage.TotalTokens != 6 {
		t.Errorf("expected TotalTokens=6, got %d", usage.TotalTokens)
	}
}

func TestClient_Health_FailsWithBadCredential(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()

	client := NewClient(srv.Client())
	err := client.Health(context.Background(), providers.Credential{APIKey: "bad", BaseURL: srv.URL})
	if err == nil {
		t.Fatal("expected Health to fail with a bad credential")
	}
}
