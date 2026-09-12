package ollama

import (
	"encoding/json"
	"testing"

	"fluxen/pkg/types"
)

func mustParse(t *testing.T, body string) *types.CanonicalRequest {
	t.Helper()
	req, err := types.ParseCanonicalRequest([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error parsing request: %v", err)
	}
	return req
}

func TestBuildOllamaRequest_PlainTextMessages(t *testing.T) {
	req := mustParse(t, `{
		"model": "llama3.2",
		"messages": [
			{"role": "system", "content": "Be terse."},
			{"role": "user", "content": "Hi there"}
		]
	}`)

	body, err := buildOllamaRequest(req, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out ollamaRequest
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if out.Model != "llama3.2" || out.Stream {
		t.Errorf("expected model=llama3.2, stream=false, got %+v", out)
	}
	if len(out.Messages) != 2 || out.Messages[0].Role != "system" || out.Messages[0].Content != "Be terse." {
		t.Errorf("expected system message preserved as plain text, got %+v", out.Messages)
	}
	if out.Messages[1].Content != "Hi there" {
		t.Errorf("expected user content Hi there, got %q", out.Messages[1].Content)
	}
}

func TestBuildOllamaRequest_ImagePartsFlattenToImagesField(t *testing.T) {
	req := mustParse(t, `{
		"model": "llava",
		"messages": [{"role": "user", "content": [
			{"type": "text", "text": "what is this?"},
			{"type": "image_url", "image_url": {"url": "data:image/png;base64,aGVsbG8="}}
		]}]
	}`)

	body, err := buildOllamaRequest(req, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out ollamaRequest
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if out.Messages[0].Content != "what is this?" {
		t.Errorf("expected text content preserved, got %q", out.Messages[0].Content)
	}
	if len(out.Messages[0].Images) != 1 || out.Messages[0].Images[0] != "aGVsbG8=" {
		t.Errorf("expected the base64 payload alone in images, got %v", out.Messages[0].Images)
	}
}

func TestBuildOllamaRequest_ToolsPassthroughAndOptions(t *testing.T) {
	req := mustParse(t, `{
		"model": "llama3.2",
		"messages": [{"role": "user", "content": "hi"}],
		"temperature": 0.7,
		"max_tokens": 256,
		"stop": ["END"],
		"tools": [{"type": "function", "function": {"name": "get_weather", "parameters": {"type": "object"}}}]
	}`)

	body, err := buildOllamaRequest(req, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out ollamaRequest
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if !out.Stream {
		t.Error("expected stream=true")
	}
	if len(out.Tools) != 1 {
		t.Fatalf("expected tools passed through verbatim, got %d entries", len(out.Tools))
	}
	if out.Options == nil || out.Options.Temperature == nil || *out.Options.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7 in options, got %+v", out.Options)
	}
	if out.Options.NumPredict == nil || *out.Options.NumPredict != 256 {
		t.Errorf("expected num_predict 256, got %+v", out.Options.NumPredict)
	}
	if len(out.Options.Stop) != 1 || out.Options.Stop[0] != "END" {
		t.Errorf("expected stop [END], got %v", out.Options.Stop)
	}
}

func TestBuildOllamaRequest_ResponseFormatBestEffortJSON(t *testing.T) {
	req := mustParse(t, `{
		"model": "llama3.2",
		"messages": [{"role": "user", "content": "hi"}],
		"response_format": {"type": "json_object"}
	}`)

	body, err := buildOllamaRequest(req, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out ollamaRequest
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	var format string
	if err := json.Unmarshal(out.Format, &format); err != nil || format != "json" {
		t.Errorf("expected format \"json\", got %s", out.Format)
	}
}
