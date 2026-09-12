package ollama

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"fluxen/pkg/types"
)

type ollamaRespMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

// ollamaResponse mirrors /api/chat's non-streaming response envelope.
// prompt_eval_count/eval_count are Ollama's own usage figures (Part
// C.6: "Ollama: prompt_eval_count/eval_count on final object") — there is
// no cost attached to them (Part D.1), only token counts.
type ollamaResponse struct {
	Model           string            `json:"model"`
	Message         ollamaRespMessage `json:"message"`
	Done            bool              `json:"done"`
	DoneReason      string            `json:"done_reason"`
	PromptEvalCount int               `json:"prompt_eval_count"`
	EvalCount       int               `json:"eval_count"`
}

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

// finishReasonFromOllama maps Ollama's done_reason onto OpenAI's finish
// reason vocabulary.
func finishReasonFromOllama(doneReason string, hasToolCalls bool) string {
	if hasToolCalls {
		return "tool_calls"
	}
	switch doneReason {
	case "length":
		return "length"
	default:
		return "stop"
	}
}

// parseOllamaResponse translates a non-streaming Ollama /api/chat
// response into a CanonicalResponse whose Raw bytes are a synthesized
// OpenAI chat.completion object.
func parseOllamaResponse(body []byte, requestedModel string) (*types.CanonicalResponse, error) {
	var o ollamaResponse
	if err := json.Unmarshal(body, &o); err != nil {
		return nil, fmt.Errorf("ollama: failed to parse response: %w", err)
	}

	var toolCalls []openAIRespToolCall
	for i, tc := range o.Message.ToolCalls {
		args := tc.Function.Arguments
		if len(args) == 0 {
			args = json.RawMessage("{}")
		}
		respTC := openAIRespToolCall{ID: fmt.Sprintf("call_%s_%d", randomID(), i)}
		respTC.Type = "function"
		respTC.Function.Name = tc.Function.Name
		respTC.Function.Arguments = string(args)
		toolCalls = append(toolCalls, respTC)
	}

	var contentPtr *string
	if o.Message.Content != "" || len(toolCalls) == 0 {
		content := o.Message.Content
		contentPtr = &content
	}

	usage := types.ResponseUsage{
		InputTokens: o.PromptEvalCount, OutputTokens: o.EvalCount, TotalTokens: o.PromptEvalCount + o.EvalCount,
	}

	model := o.Model
	if model == "" {
		model = requestedModel
	}

	id := "chatcmpl-" + randomID()
	out := openAIChatCompletion{
		ID: id, Object: "chat.completion", Created: time.Now().Unix(), Model: model,
		Choices: []openAIChoice{{
			Index:        0,
			Message:      openAIRespMessage{Role: "assistant", Content: contentPtr, ToolCalls: toolCalls},
			FinishReason: finishReasonFromOllama(o.DoneReason, len(toolCalls) > 0),
		}},
		Usage: &openAIUsageObject{PromptTokens: usage.InputTokens, CompletionTokens: usage.OutputTokens, TotalTokens: usage.TotalTokens},
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("ollama: failed to synthesize openai-shaped response: %w", err)
	}
	return &types.CanonicalResponse{ID: id, Model: model, Usage: usage, Raw: raw}, nil
}

func randomID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
