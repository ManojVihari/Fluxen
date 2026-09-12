package gemini

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"fluxen/pkg/providers"
)

// TestParseGeminiResponse_GoldenChatCompletion is the golden test for
// Gemini non-streaming response translation (Part K's Testing Strategy;
// Phase 7's "golden tests for Gemini and Ollama translation, both
// directions").
func TestParseGeminiResponse_GoldenChatCompletion(t *testing.T) {
	body, err := os.ReadFile("testdata/generate_content_response.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	resp, err := parseGeminiResponse(body, "gemini-1.5-flash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Model != "gemini-1.5-flash" {
		t.Errorf("expected model gemini-1.5-flash, got %q", resp.Model)
	}
	if resp.Usage.InputTokens != 12 || resp.Usage.OutputTokens != 4 || resp.Usage.TotalTokens != 16 {
		t.Errorf("expected usage 12/4/16, got %+v", resp.Usage)
	}

	var out openAIChatCompletion
	if err := json.Unmarshal(resp.Raw, &out); err != nil {
		t.Fatalf("Raw is not valid OpenAI-shaped JSON: %v", err)
	}
	if out.Object != "chat.completion" {
		t.Errorf("expected object chat.completion, got %q", out.Object)
	}
	if len(out.Choices) != 1 {
		t.Fatalf("expected exactly one choice, got %d", len(out.Choices))
	}
	choice := out.Choices[0]
	if choice.Message.Content == nil || *choice.Message.Content != "Hello there!" {
		t.Errorf("expected content %q, got %v", "Hello there!", choice.Message.Content)
	}
	if choice.FinishReason != "stop" {
		t.Errorf("expected finish_reason stop, got %q", choice.FinishReason)
	}
	if out.Usage == nil || out.Usage.PromptTokens != 12 || out.Usage.CompletionTokens != 4 {
		t.Errorf("expected usage object in the synthesized response, got %+v", out.Usage)
	}
}

// TestParseGeminiResponse_SafetyBlockBecomesUpstreamError is the safety-
// block handling test: a prompt Gemini refused to generate against at
// all must surface as a rejected request, not a silently-empty 200.
func TestParseGeminiResponse_SafetyBlockBecomesUpstreamError(t *testing.T) {
	body, err := os.ReadFile("testdata/safety_block_response.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	_, err = parseGeminiResponse(body, "gemini-1.5-flash")
	if err == nil {
		t.Fatal("expected an error for a safety-blocked prompt")
	}
	var upstreamErr *providers.UpstreamError
	if !errors.As(err, &upstreamErr) {
		t.Fatalf("expected a *providers.UpstreamError, got %T", err)
	}
	if upstreamErr.StatusCode != 400 {
		t.Errorf("expected status 400, got %d", upstreamErr.StatusCode)
	}

	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(upstreamErr.Body, &env); err != nil {
		t.Fatalf("error body is not OpenAI-shaped JSON: %v", err)
	}
	if env.Error.Code != "content_filter" {
		t.Errorf("expected error code content_filter, got %q", env.Error.Code)
	}
}

func TestFinishReasonFromGemini(t *testing.T) {
	cases := []struct {
		geminiReason    string
		hasFunctionCall bool
		want            string
	}{
		{"STOP", false, "stop"},
		{"MAX_TOKENS", false, "length"},
		{"SAFETY", false, "content_filter"},
		{"RECITATION", false, "content_filter"},
		{"STOP", true, "tool_calls"},
	}
	for _, c := range cases {
		got := finishReasonFromGemini(c.geminiReason, c.hasFunctionCall)
		if got != c.want {
			t.Errorf("finishReasonFromGemini(%q, %v) = %q, want %q", c.geminiReason, c.hasFunctionCall, got, c.want)
		}
	}
}
