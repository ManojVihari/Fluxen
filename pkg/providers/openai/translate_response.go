package openai

import (
	"encoding/json"
	"fmt"

	"fluxen/pkg/types"
)

type openAIUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

type openAIResponseEnvelope struct {
	ID    string       `json:"id"`
	Model string       `json:"model"`
	Usage *openAIUsage `json:"usage"`
}

// parseResponse extracts accounting fields from a non-streaming OpenAI
// response without discarding anything — Raw carries the exact bytes the
// gateway relays to the client (Part C.3's CanonicalResponse doc comment).
func parseResponse(body []byte) (*types.CanonicalResponse, error) {
	var env openAIResponseEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("openai: failed to parse response: %w", err)
	}

	resp := &types.CanonicalResponse{
		ID:    env.ID,
		Model: env.Model,
		Raw:   body,
	}
	if env.Usage != nil {
		resp.Usage = types.ResponseUsage{
			InputTokens:       env.Usage.PromptTokens,
			OutputTokens:      env.Usage.CompletionTokens,
			TotalTokens:       env.Usage.TotalTokens,
			CachedInputTokens: env.Usage.PromptTokensDetails.CachedTokens,
			ReasoningTokens:   env.Usage.CompletionTokensDetails.ReasoningTokens,
		}
	}
	return resp, nil
}
