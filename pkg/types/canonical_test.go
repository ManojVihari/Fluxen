package types

import (
	"encoding/json"
	"testing"
)

func TestParseCanonicalRequest_MissingModelFails(t *testing.T) {
	_, err := ParseCanonicalRequest([]byte(`{"messages":[]}`))
	if err == nil {
		t.Fatal("expected an error when \"model\" is missing")
	}
}

func TestParseCanonicalRequest_InvalidJSONFails(t *testing.T) {
	_, err := ParseCanonicalRequest([]byte(`not json`))
	if err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}

func TestParseCanonicalRequest_KnownFieldsExtracted(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o-mini",
		"messages": [{"role":"user","content":"hi"}],
		"temperature": 0.7,
		"max_tokens": 256,
		"stream": true,
		"stop": ["\n"]
	}`)

	req, err := ParseCanonicalRequest(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Model != "gpt-4o-mini" {
		t.Errorf("expected model gpt-4o-mini, got %q", req.Model)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(req.Messages))
	}
	if req.Temperature == nil || *req.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", req.Temperature)
	}
	if req.MaxTokens == nil || *req.MaxTokens != 256 {
		t.Errorf("expected max_tokens 256, got %v", req.MaxTokens)
	}
	if !req.Stream {
		t.Error("expected stream=true")
	}
	if len(req.Stop) != 1 || req.Stop[0] != "\n" {
		t.Errorf("expected stop=[\"\\n\"], got %v", req.Stop)
	}
}

func TestParseCanonicalRequest_StopAsSingleString(t *testing.T) {
	body := []byte(`{"model":"gpt-4o-mini","messages":[],"stop":"END"}`)
	req, err := ParseCanonicalRequest(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(req.Stop) != 1 || req.Stop[0] != "END" {
		t.Errorf("expected stop=[\"END\"], got %v", req.Stop)
	}
}

// TestRoundTrip_UnknownFieldsSurvive is the load-bearing test for the
// "unknown fields pass through untouched" guarantee (Part C.4): a field
// Fluxen doesn't model explicitly must still be present, byte-identical,
// after Parse -> JSON.
func TestRoundTrip_UnknownFieldsSurvive(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role":"user","content":"hi"}],
		"presence_penalty": 0.5,
		"user": "user-123",
		"parallel_tool_calls": false,
		"logprobs": true,
		"stream_options": {"include_usage": true}
	}`)

	req, err := ParseCanonicalRequest(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out, err := req.JSON()
	if err != nil {
		t.Fatalf("unexpected error marshaling: %v", err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	for _, key := range []string{"presence_penalty", "user", "parallel_tool_calls", "logprobs", "stream_options"} {
		if _, ok := got[key]; !ok {
			t.Errorf("expected unknown field %q to survive round-trip, but it's missing", key)
		}
	}

	var userVal string
	if err := json.Unmarshal(got["user"], &userVal); err != nil || userVal != "user-123" {
		t.Errorf("expected user=%q to survive unchanged, got %s (err=%v)", "user-123", got["user"], err)
	}
}

func TestJSON_KnownFieldsReembedded(t *testing.T) {
	temp := 0.3
	req := &CanonicalRequest{
		Model:       "gpt-4o-mini",
		Messages:    []Message{json.RawMessage(`{"role":"user","content":"hi"}`)},
		Temperature: &temp,
		Extra:       map[string]json.RawMessage{},
	}

	out, err := req.JSON()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if _, ok := got["model"]; !ok {
		t.Error("expected \"model\" to be present in output")
	}
	if _, ok := got["messages"]; !ok {
		t.Error("expected \"messages\" to be present in output")
	}
	if _, ok := got["temperature"]; !ok {
		t.Error("expected \"temperature\" to be present in output")
	}
}
