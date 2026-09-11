package openai

import (
	"os"
	"testing"
)

// TestParseResponse_GoldenChatCompletion is the golden test for OpenAI
// non-streaming response translation (Part K's Testing Strategy).
func TestParseResponse_GoldenChatCompletion(t *testing.T) {
	body, err := os.ReadFile("testdata/chat_response.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	resp, err := parseResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.ID != "chatcmpl-abc123" {
		t.Errorf("expected id chatcmpl-abc123, got %q", resp.ID)
	}
	if resp.Model != "gpt-4o-mini" {
		t.Errorf("expected model gpt-4o-mini, got %q", resp.Model)
	}
	if resp.Usage.InputTokens != 12 {
		t.Errorf("expected InputTokens=12, got %d", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 4 {
		t.Errorf("expected OutputTokens=4, got %d", resp.Usage.OutputTokens)
	}
	if resp.Usage.TotalTokens != 16 {
		t.Errorf("expected TotalTokens=16, got %d", resp.Usage.TotalTokens)
	}
	if string(resp.Raw) != string(body) {
		t.Error("expected Raw to hold the exact original response bytes")
	}
}
