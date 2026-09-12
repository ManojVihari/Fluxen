package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"fluxen/pkg/providers"
	"fluxen/pkg/types"
)

// geminiErrorEnvelope is Gemini's own error shape: {"error":{"code":...,
// "message":...,"status":...}}.
type geminiErrorEnvelope struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// translateError re-shapes a Gemini error body into the OpenAI error
// envelope every Fluxen client expects (errorEnvelope in
// internal/gateway/errors.go) — Part D's "providers with a genuinely
// different error dialect ... translate Body into the client's dialect
// before relaying it."
func translateError(statusCode int, body []byte) *providers.UpstreamError {
	var env geminiErrorEnvelope
	message := "The upstream provider returned an error."
	if err := json.Unmarshal(body, &env); err == nil && env.Error.Message != "" {
		message = env.Error.Message
	}
	out, _ := json.Marshal(map[string]any{
		"error": map[string]string{"message": message, "type": "upstream_error", "code": "gemini_error"},
	})
	return &providers.UpstreamError{StatusCode: statusCode, Body: out}
}

// Chat performs a non-streaming generateContent call.
func (c *Client) Chat(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (*types.CanonicalResponse, error) {
	body, err := buildGeminiRequest(req)
	if err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("%s/v1beta/models/%s:generateContent", baseURL(cred), url.PathEscape(req.Model))
	httpReq, err := newRequest(ctx, http.MethodPost, endpoint, bytes.NewReader(body), cred)
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

	return parseGeminiResponse(respBody, req.Model)
}

// Models returns Gemini's static-per-deployment model list. Part D: "✅
// static catalog" for Gemini (unlike OpenAI's live /v1/models
// passthrough or Ollama's live /api/tags) — this deliberately does not
// call Gemini's own ListModels endpoint, matching that frozen scope.
func (c *Client) Models(ctx context.Context, cred providers.Credential) ([]providers.ModelInfo, error) {
	return []providers.ModelInfo{
		{ID: "gemini-1.5-pro"}, {ID: "gemini-1.5-flash"}, {ID: "gemini-1.5-flash-8b"}, {ID: "gemini-2.0-flash"},
	}, nil
}

// Health confirms the credential can reach Gemini by listing real
// models via the API (distinct from Models(), which returns Fluxen's own
// static catalog) — an invalid key fails this call even though Models()
// itself makes no network request.
func (c *Client) Health(ctx context.Context, cred providers.Credential) error {
	endpoint := baseURL(cred) + "/v1beta/models"
	httpReq, err := newRequest(ctx, http.MethodGet, endpoint, nil, cred)
	if err != nil {
		return err
	}
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(httpResp.Body)
		return translateError(httpResp.StatusCode, respBody)
	}
	return nil
}
