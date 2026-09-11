package types

import "testing"

func TestExtractWorkloadFeatures_ToolsAndImages(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "system", "content": "be helpful"},
			{"role": "user", "content": [
				{"type": "text", "text": "what is this?"},
				{"type": "image_url", "image_url": {"url": "https://example.com/x.png"}}
			]}
		],
		"tools": [{"type": "function", "function": {"name": "lookup"}}]
	}`)

	req, err := ParseCanonicalRequest(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	f := ExtractWorkloadFeatures(req)

	if !f.HasTools {
		t.Error("expected HasTools=true")
	}
	if !f.HasImages {
		t.Error("expected HasImages=true")
	}
	if f.HasToolCalls {
		t.Error("expected HasToolCalls=false (no tool_calls or tool-role messages)")
	}
	if f.MessageCount != 2 {
		t.Errorf("expected MessageCount=2, got %d", f.MessageCount)
	}
	if f.SystemPromptHash == nil {
		t.Error("expected SystemPromptHash to be set from the system message")
	}
}

func TestExtractWorkloadFeatures_ToolCallsDetected(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "user", "content": "run the tool"},
			{"role": "assistant", "content": null, "tool_calls": [{"id":"1","type":"function","function":{"name":"x","arguments":"{}"}}]},
			{"role": "tool", "content": "result", "tool_call_id": "1"}
		]
	}`)

	req, err := ParseCanonicalRequest(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	f := ExtractWorkloadFeatures(req)
	if !f.HasToolCalls {
		t.Error("expected HasToolCalls=true")
	}
}

func TestExtractWorkloadFeatures_JSONMode(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role":"user","content":"give me json"}],
		"response_format": {"type": "json_object"}
	}`)

	req, err := ParseCanonicalRequest(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	f := ExtractWorkloadFeatures(req)
	if !f.JSONMode {
		t.Error("expected JSONMode=true")
	}
}

func TestExtractWorkloadFeatures_NoSystemMessageNoHash(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	req, err := ParseCanonicalRequest(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := ExtractWorkloadFeatures(req)
	if f.SystemPromptHash != nil {
		t.Error("expected SystemPromptHash to be nil when there's no system message")
	}
}
