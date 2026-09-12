package ollama

import (
	"encoding/json"
	"os"
	"testing"
)

// TestParseOllamaResponse_GoldenChatCompletion is the golden test for
// Ollama non-streaming response translation (Phase 7's "golden tests for
// Gemini and Ollama translation, both directions").
func TestParseOllamaResponse_GoldenChatCompletion(t *testing.T) {
	body, err := os.ReadFile("testdata/chat_response.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	resp, err := parseOllamaResponse(body, "llama3.2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Model != "llama3.2:latest" {
		t.Errorf("expected model llama3.2:latest (echoed from Ollama), got %q", resp.Model)
	}
	if resp.Usage.InputTokens != 12 || resp.Usage.OutputTokens != 4 || resp.Usage.TotalTokens != 16 {
		t.Errorf("expected usage 12/4/16 from prompt_eval_count/eval_count, got %+v", resp.Usage)
	}

	var out openAIChatCompletion
	if err := json.Unmarshal(resp.Raw, &out); err != nil {
		t.Fatalf("Raw is not valid OpenAI-shaped JSON: %v", err)
	}
	if out.Object != "chat.completion" {
		t.Errorf("expected object chat.completion, got %q", out.Object)
	}
	if len(out.Choices) != 1 || out.Choices[0].Message.Content == nil || *out.Choices[0].Message.Content != "Hello there!" {
		t.Errorf("expected content Hello there!, got %+v", out.Choices)
	}
	if out.Choices[0].FinishReason != "stop" {
		t.Errorf("expected finish_reason stop, got %q", out.Choices[0].FinishReason)
	}
}

func TestParseOllamaResponse_ToolCallArgumentsSerializedAsString(t *testing.T) {
	body := []byte(`{
		"model": "llama3.2",
		"message": {"role": "assistant", "content": "", "tool_calls": [
			{"function": {"name": "get_weather", "arguments": {"city": "Boston"}}}
		]},
		"done": true, "done_reason": "stop",
		"prompt_eval_count": 20, "eval_count": 6
	}`)

	resp, err := parseOllamaResponse(body, "llama3.2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out openAIChatCompletion
	if err := json.Unmarshal(resp.Raw, &out); err != nil {
		t.Fatalf("Raw is not valid JSON: %v", err)
	}
	if len(out.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(out.Choices[0].Message.ToolCalls))
	}
	tc := out.Choices[0].Message.ToolCalls[0]
	if tc.Function.Name != "get_weather" {
		t.Errorf("expected function name get_weather, got %q", tc.Function.Name)
	}
	// OpenAI's dialect requires arguments as a JSON-encoded *string*, not
	// a nested object the way Ollama's own dialect represents it.
	var args map[string]string
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("expected arguments to be a JSON string containing an object, got %q: %v", tc.Function.Arguments, err)
	}
	if args["city"] != "Boston" {
		t.Errorf("expected city=Boston, got %+v", args)
	}
	if out.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("expected finish_reason tool_calls, got %q", out.Choices[0].FinishReason)
	}
}
