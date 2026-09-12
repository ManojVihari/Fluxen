// Package ollama implements pkg/providers.Provider for a self-hosted
// Ollama instance's native /api/chat and /api/tags endpoints (not
// Ollama's own OpenAI-compatibility shim — using the native API keeps
// Fluxen in direct control of usage extraction (prompt_eval_count/
// eval_count) and model discovery, matching Part D.1's frozen cost
// handling). As with pkg/providers/gemini, translation is real in both
// directions: the client only ever speaks OpenAI's dialect to Fluxen
// (Part A.3), regardless of which of the three providers actually served
// the request.
package ollama

import (
	"context"
	"io"
	"net/http"
	"time"

	"fluxen/pkg/providers"
)

// defaultBaseURL is Ollama's own default local listen address.
const defaultBaseURL = "http://localhost:11434"

// DefaultTimeout is generous relative to OpenAI/Gemini's 120s: local
// models on modest hardware can be materially slower per token, and
// there is no cost pressure to bound the wait more tightly the way an
// API bill would.
const DefaultTimeout = 300 * time.Second

// Client implements providers.Provider for Ollama.
type Client struct {
	httpClient *http.Client
}

// NewClient returns an Ollama provider client. httpClient may be nil, in
// which case a client with DefaultTimeout is used.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultTimeout}
	}
	return &Client{httpClient: httpClient}
}

func (c *Client) Name() string { return "ollama" }

func baseURL(cred providers.Credential) string {
	if cred.BaseURL != "" {
		return cred.BaseURL
	}
	return defaultBaseURL
}

// newRequest builds an Ollama API request. A stock Ollama install has no
// authentication at all (Credential.APIKey is simply omitted in that
// case); when APIKey is set (a proxied or authenticated Ollama
// deployment) it's sent as a Bearer token, the closest common
// convention.
func newRequest(ctx context.Context, method, url string, body io.Reader, cred providers.Credential) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cred.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cred.APIKey)
	}
	return req, nil
}
