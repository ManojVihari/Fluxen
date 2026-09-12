package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"fluxen/pkg/providers"
	"fluxen/pkg/types"
)

// ollamaErrorEnvelope is Ollama's own error shape: {"error": "message"}.
type ollamaErrorEnvelope struct {
	Error string `json:"error"`
}

// translateError re-shapes an Ollama error body into the OpenAI error
// envelope every Fluxen client expects (Part D: providers with a
// different error dialect translate before relaying).
func translateError(statusCode int, body []byte) *providers.UpstreamError {
	var env ollamaErrorEnvelope
	message := "The upstream provider returned an error."
	if err := json.Unmarshal(body, &env); err == nil && env.Error != "" {
		message = env.Error
	}
	out, _ := json.Marshal(map[string]any{
		"error": map[string]string{"message": message, "type": "upstream_error", "code": "ollama_error"},
	})
	return &providers.UpstreamError{StatusCode: statusCode, Body: out}
}

// Chat performs a non-streaming /api/chat call.
func (c *Client) Chat(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (*types.CanonicalResponse, error) {
	body, err := buildOllamaRequest(req, false)
	if err != nil {
		return nil, err
	}

	httpReq, err := newRequest(ctx, http.MethodPost, baseURL(cred)+"/api/chat", bytes.NewReader(body), cred)
	if err != nil {
		return nil, err
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, translateError(httpResp.StatusCode, respBody)
	}

	return parseOllamaResponse(respBody, req.Model)
}

type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// Models lists locally-installed models via Ollama's live /api/tags
// endpoint (Part D: "✅ live via /api/tags" — unlike Gemini's static
// per-deployment catalog, Ollama's model set is whatever the operator
// has pulled onto that host, which only that host can enumerate).
func (c *Client) Models(ctx context.Context, cred providers.Credential) ([]providers.ModelInfo, error) {
	httpReq, err := newRequest(ctx, http.MethodGet, baseURL(cred)+"/api/tags", nil, cred)
	if err != nil {
		return nil, err
	}
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, translateError(httpResp.StatusCode, respBody)
	}

	var parsed ollamaTagsResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}
	models := make([]providers.ModelInfo, 0, len(parsed.Models))
	for _, m := range parsed.Models {
		models = append(models, providers.ModelInfo{ID: m.Name})
	}
	return models, nil
}

// Health confirms the configured Ollama instance is reachable by hitting
// /api/tags — cheap, always available, and doesn't load a model the way
// a real chat call would.
func (c *Client) Health(ctx context.Context, cred providers.Credential) error {
	_, err := c.Models(ctx, cred)
	return err
}
