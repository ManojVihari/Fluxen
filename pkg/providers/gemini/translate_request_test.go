package gemini

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

func TestBuildGeminiRequest_SystemAndUserMessages(t *testing.T) {
	req := mustParse(t, `{
		"model": "gemini-1.5-flash",
		"messages": [
			{"role": "system", "content": "You are terse."},
			{"role": "user", "content": "Hi there"}
		]
	}`)

	body, err := buildGeminiRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out geminiRequest
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if out.SystemInstruction == nil || len(out.SystemInstruction.Parts) != 1 || out.SystemInstruction.Parts[0].Text != "You are terse." {
		t.Errorf("expected system instruction to carry the system message, got %+v", out.SystemInstruction)
	}
	if len(out.Contents) != 1 || out.Contents[0].Role != "user" || out.Contents[0].Parts[0].Text != "Hi there" {
		t.Errorf("expected one user content turn, got %+v", out.Contents)
	}
}

func TestBuildGeminiRequest_AssistantRoleBecomesModel(t *testing.T) {
	req := mustParse(t, `{
		"model": "gemini-1.5-flash",
		"messages": [
			{"role": "user", "content": "Hi"},
			{"role": "assistant", "content": "Hello!"}
		]
	}`)

	body, err := buildGeminiRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out geminiRequest
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(out.Contents) != 2 || out.Contents[1].Role != "model" {
		t.Errorf("expected the assistant turn translated to role=model, got %+v", out.Contents)
	}
}

func TestBuildGeminiRequest_ToolCallsAndResponsesRoundTrip(t *testing.T) {
	req := mustParse(t, `{
		"model": "gemini-1.5-flash",
		"messages": [
			{"role": "user", "content": "What's the weather in Boston?"},
			{"role": "assistant", "content": null, "tool_calls": [
				{"id": "call_1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"Boston\"}"}}
			]},
			{"role": "tool", "tool_call_id": "call_1", "content": "72F and sunny"}
		],
		"tools": [
			{"type": "function", "function": {"name": "get_weather", "description": "Get weather", "parameters": {"type": "object"}}}
		]
	}`)

	body, err := buildGeminiRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out geminiRequest
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if len(out.Tools) != 1 || len(out.Tools[0].FunctionDeclarations) != 1 || out.Tools[0].FunctionDeclarations[0].Name != "get_weather" {
		t.Fatalf("expected one function declaration, got %+v", out.Tools)
	}

	if len(out.Contents) != 3 {
		t.Fatalf("expected 3 content turns, got %d: %+v", len(out.Contents), out.Contents)
	}
	modelTurn := out.Contents[1]
	if modelTurn.Role != "model" || len(modelTurn.Parts) != 1 || modelTurn.Parts[0].FunctionCall == nil || modelTurn.Parts[0].FunctionCall.Name != "get_weather" {
		t.Errorf("expected a functionCall part naming get_weather, got %+v", modelTurn)
	}

	toolTurn := out.Contents[2]
	if toolTurn.Role != "user" || len(toolTurn.Parts) != 1 || toolTurn.Parts[0].FunctionResponse == nil {
		t.Fatalf("expected a functionResponse part, got %+v", toolTurn)
	}
	if toolTurn.Parts[0].FunctionResponse.Name != "get_weather" {
		t.Errorf("expected the tool response to recover the function name from the matching tool_call id, got %q", toolTurn.Parts[0].FunctionResponse.Name)
	}
}

func TestBuildGeminiRequest_GenerationConfigAndResponseFormat(t *testing.T) {
	temp := 0.5
	req := mustParse(t, `{
		"model": "gemini-1.5-flash",
		"messages": [{"role": "user", "content": "hi"}],
		"temperature": 0.5,
		"max_tokens": 100,
		"stop": ["END"],
		"response_format": {"type": "json_schema", "json_schema": {"schema": {"type": "object"}}}
	}`)
	_ = temp

	body, err := buildGeminiRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out geminiRequest
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if out.GenerationConfig == nil {
		t.Fatal("expected a generationConfig")
	}
	if out.GenerationConfig.Temperature == nil || *out.GenerationConfig.Temperature != 0.5 {
		t.Errorf("expected temperature 0.5, got %+v", out.GenerationConfig.Temperature)
	}
	if out.GenerationConfig.MaxOutputTokens == nil || *out.GenerationConfig.MaxOutputTokens != 100 {
		t.Errorf("expected maxOutputTokens 100, got %+v", out.GenerationConfig.MaxOutputTokens)
	}
	if len(out.GenerationConfig.StopSequences) != 1 || out.GenerationConfig.StopSequences[0] != "END" {
		t.Errorf("expected stopSequences [END], got %v", out.GenerationConfig.StopSequences)
	}
	if out.GenerationConfig.ResponseMimeType != "application/json" {
		t.Errorf("expected application/json responseMimeType, got %q", out.GenerationConfig.ResponseMimeType)
	}
	if len(out.GenerationConfig.ResponseSchema) == 0 {
		t.Error("expected a responseSchema to be carried through")
	}
}

func TestBuildGeminiRequest_DataURIImageBecomesInlineData(t *testing.T) {
	req := mustParse(t, `{
		"model": "gemini-1.5-flash",
		"messages": [{"role": "user", "content": [
			{"type": "text", "text": "what is this?"},
			{"type": "image_url", "image_url": {"url": "data:image/png;base64,aGVsbG8="}}
		]}]
	}`)

	body, err := buildGeminiRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out geminiRequest
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(out.Contents) != 1 || len(out.Contents[0].Parts) != 2 {
		t.Fatalf("expected 2 parts (text + inlineData), got %+v", out.Contents)
	}
	img := out.Contents[0].Parts[1]
	if img.InlineData == nil || img.InlineData.MimeType != "image/png" || img.InlineData.Data != "aGVsbG8=" {
		t.Errorf("expected inlineData with the decoded mime type and base64 payload, got %+v", img.InlineData)
	}
}

func TestBuildGeminiRequest_RemoteImageURLDropped(t *testing.T) {
	req := mustParse(t, `{
		"model": "gemini-1.5-flash",
		"messages": [{"role": "user", "content": [
			{"type": "text", "text": "what is this?"},
			{"type": "image_url", "image_url": {"url": "https://example.com/cat.png"}}
		]}]
	}`)

	body, err := buildGeminiRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out geminiRequest
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(out.Contents[0].Parts) != 1 {
		t.Errorf("expected the remote image part to be dropped, kept only text; got %+v", out.Contents[0].Parts)
	}
}
