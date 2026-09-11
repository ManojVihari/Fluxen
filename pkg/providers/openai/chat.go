package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"fluxen/pkg/providers"
	"fluxen/pkg/types"
)

// Chat performs a non-streaming chat completion call.
func (c *Client) Chat(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (*types.CanonicalResponse, error) {
	body, err := buildRequestBody(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := newRequest(ctx, http.MethodPost, baseURL(cred)+"/v1/chat/completions", bytes.NewReader(body), cred)
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
		return nil, &providers.UpstreamError{StatusCode: httpResp.StatusCode, Body: respBody}
	}

	return parseResponse(respBody)
}

// Models lists the models visible to this credential.
func (c *Client) Models(ctx context.Context, cred providers.Credential) ([]providers.ModelInfo, error) {
	httpReq, err := newRequest(ctx, http.MethodGet, baseURL(cred)+"/v1/models", nil, cred)
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
		return nil, &providers.UpstreamError{StatusCode: httpResp.StatusCode, Body: respBody}
	}

	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}

	models := make([]providers.ModelInfo, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		models = append(models, providers.ModelInfo{ID: m.ID})
	}
	return models, nil
}

// Health confirms the credential can reach OpenAI by listing models.
func (c *Client) Health(ctx context.Context, cred providers.Credential) error {
	_, err := c.Models(ctx, cred)
	return err
}
