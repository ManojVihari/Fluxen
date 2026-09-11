// Package openai implements pkg/providers.Provider for OpenAI's chat
// completions API. Translation is near-passthrough: CanonicalRequest is
// already shaped like an OpenAI request (Part C.3), so this package's job
// is mostly to forward it, inject stream_options.include_usage when
// streaming, and extract usage for accounting — not to reconstruct a
// different wire dialect.
package openai

import (
	"context"
	"io"
	"net/http"
	"time"

	"fluxen/pkg/providers"
)

const defaultBaseURL = "https://api.openai.com"

// DefaultTimeout is the upstream call timeout applied when the caller's
// context has no earlier deadline (Phase 1 backend task: "Upstream
// timeout handling (default 120s)").
const DefaultTimeout = 120 * time.Second

// Client implements providers.Provider for OpenAI.
type Client struct {
	httpClient *http.Client
}

// NewClient returns an OpenAI provider client. httpClient may be nil, in
// which case a client with DefaultTimeout is used; tests and the gateway
// pass one with a shorter timeout or pointed at a mock server via
// Credential.BaseURL.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultTimeout}
	}
	return &Client{httpClient: httpClient}
}

func (c *Client) Name() string { return "openai" }

func baseURL(cred providers.Credential) string {
	if cred.BaseURL != "" {
		return cred.BaseURL
	}
	return defaultBaseURL
}

func newRequest(ctx context.Context, method, url string, body io.Reader, cred providers.Credential) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cred.APIKey)
	return req, nil
}
