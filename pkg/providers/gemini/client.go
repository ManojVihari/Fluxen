// Package gemini implements pkg/providers.Provider for Google Gemini's
// generateContent API. Unlike pkg/providers/openai, translation here is
// real: CanonicalRequest/CanonicalResponse are OpenAI-shaped (Part C.3),
// and Gemini's wire dialect is genuinely different (contents/parts,
// systemInstruction, usageMetadata, finishReason vocabulary) — this
// package's job is to translate faithfully in both directions so a
// client that only ever speaks OpenAI's dialect to Fluxen (Part A.3:
// "OpenAI-compatible ingress ... routing to OpenAI/Gemini/Ollama") never
// has to know Gemini served the request.
package gemini

import (
	"context"
	"io"
	"net/http"
	"time"

	"fluxen/pkg/providers"
)

const defaultBaseURL = "https://generativelanguage.googleapis.com"

// DefaultTimeout mirrors pkg/providers/openai's own upstream call
// timeout (Part C.5's 120s default), applied when the caller's context
// has no earlier deadline.
const DefaultTimeout = 120 * time.Second

// Client implements providers.Provider for Gemini.
type Client struct {
	httpClient *http.Client
}

// NewClient returns a Gemini provider client. httpClient may be nil, in
// which case a client with DefaultTimeout is used.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultTimeout}
	}
	return &Client{httpClient: httpClient}
}

func (c *Client) Name() string { return "gemini" }

func baseURL(cred providers.Credential) string {
	if cred.BaseURL != "" {
		return cred.BaseURL
	}
	return defaultBaseURL
}

// newRequest builds an authenticated Gemini API request. Gemini
// authenticates via the x-goog-api-key header rather than OpenAI's
// Bearer scheme (Part D: providers are not pretended to be identical).
func newRequest(ctx context.Context, method, url string, body io.Reader, cred providers.Credential) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", cred.APIKey)
	return req, nil
}
