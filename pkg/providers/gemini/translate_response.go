package gemini

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"fluxen/pkg/providers"
	"fluxen/pkg/types"
)

type geminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
	Index        int           `json:"index"`
}

type geminiPromptFeedback struct {
	BlockReason string `json:"blockReason"`
}

type geminiResponse struct {
	Candidates     []geminiCandidate     `json:"candidates"`
	UsageMetadata  *geminiUsageMetadata  `json:"usageMetadata"`
	PromptFeedback *geminiPromptFeedback `json:"promptFeedback"`
	ModelVersion   string                `json:"modelVersion"`
}

// openAIChatCompletion is the OpenAI-shaped envelope this package
// synthesizes from a Gemini response — the only dialect a client of
// Fluxen's single OpenAI-compatible endpoint ever sees (Part A.3),
// regardless of which of the three providers actually served the
// request.
type openAIChatCompletion struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []openAIChoice     `json:"choices"`
	Usage   *openAIUsageObject `json:"usage,omitempty"`
}

type openAIChoice struct {
	Index        int               `json:"index"`
	Message      openAIRespMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
}

type openAIRespMessage struct {
	Role      string               `json:"role"`
	Content   *string              `json:"content"`
	ToolCalls []openAIRespToolCall `json:"tool_calls,omitempty"`
}

type openAIRespToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIUsageObject struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// finishReasonFromGemini maps Gemini's finishReason vocabulary onto
// OpenAI's (Part C.6/D: providers are not pretended to be identical, but
// the client only ever understands OpenAI's four values).
func finishReasonFromGemini(reason string, hasFunctionCall bool) string {
	if hasFunctionCall {
		return "tool_calls"
	}
	switch reason {
	case "MAX_TOKENS":
		return "length"
	case "SAFETY", "RECITATION", "OTHER", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
		return "content_filter"
	case "STOP", "":
		return "stop"
	default:
		return "stop"
	}
}

// safetyBlockError builds an OpenAI-shaped error for a prompt Gemini
// refused to generate against at all (promptFeedback.blockReason set,
// zero candidates) — Fluxen's own safety-block handling (Phase 7 backend
// task). This mirrors how OpenAI itself reports a moderation-flagged
// request: as a rejected call, not a 200 with silently empty content.
func safetyBlockError(blockReason string) []byte {
	body, _ := json.Marshal(map[string]any{
		"error": map[string]string{
			"message": fmt.Sprintf("The request was blocked by Gemini's safety filters (%s).", blockReason),
			"type":    "content_filter_error",
			"code":    "content_filter",
		},
	})
	return body
}

// parseGeminiResponse translates a non-streaming Gemini generateContent
// response into a CanonicalResponse whose Raw bytes are a synthesized
// OpenAI chat.completion object — not a passthrough, unlike
// pkg/providers/openai's parseResponse, because the client never speaks
// Gemini's dialect.
func parseGeminiResponse(body []byte, requestedModel string) (*types.CanonicalResponse, error) {
	var g geminiResponse
	if err := json.Unmarshal(body, &g); err != nil {
		return nil, fmt.Errorf("gemini: failed to parse response: %w", err)
	}

	if len(g.Candidates) == 0 {
		if g.PromptFeedback != nil && g.PromptFeedback.BlockReason != "" {
			return nil, &providers.UpstreamError{StatusCode: http.StatusBadRequest, Body: safetyBlockError(g.PromptFeedback.BlockReason)}
		}
		return nil, fmt.Errorf("gemini: response contained no candidates")
	}

	cand := g.Candidates[0]
	var textContent string
	var toolCalls []openAIRespToolCall
	for i, part := range cand.Content.Parts {
		if part.Text != "" {
			textContent += part.Text
		}
		if part.FunctionCall != nil {
			args := part.FunctionCall.Args
			if len(args) == 0 {
				args = json.RawMessage("{}")
			}
			tc := openAIRespToolCall{ID: fmt.Sprintf("call_%s_%d", randomID(), i)}
			tc.Type = "function"
			tc.Function.Name = part.FunctionCall.Name
			tc.Function.Arguments = string(args)
			toolCalls = append(toolCalls, tc)
		}
	}

	var contentPtr *string
	if textContent != "" || len(toolCalls) == 0 {
		contentPtr = &textContent
	}

	finishReason := finishReasonFromGemini(cand.FinishReason, len(toolCalls) > 0)

	usage := types.ResponseUsage{}
	openAIUsage := &openAIUsageObject{}
	if g.UsageMetadata != nil {
		usage = types.ResponseUsage{
			InputTokens: g.UsageMetadata.PromptTokenCount, OutputTokens: g.UsageMetadata.CandidatesTokenCount,
			TotalTokens: g.UsageMetadata.TotalTokenCount,
		}
		openAIUsage.PromptTokens, openAIUsage.CompletionTokens, openAIUsage.TotalTokens = usage.InputTokens, usage.OutputTokens, usage.TotalTokens
	}

	id := "chatcmpl-" + randomID()
	out := openAIChatCompletion{
		ID: id, Object: "chat.completion", Created: time.Now().Unix(), Model: requestedModel,
		Choices: []openAIChoice{{
			Index:        cand.Index,
			Message:      openAIRespMessage{Role: "assistant", Content: contentPtr, ToolCalls: toolCalls},
			FinishReason: finishReason,
		}},
		Usage: openAIUsage,
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("gemini: failed to synthesize openai-shaped response: %w", err)
	}

	return &types.CanonicalResponse{ID: id, Model: requestedModel, Usage: usage, Raw: raw}, nil
}

func randomID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
