package openai

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

func TestBuildRequestBody_InjectsStreamUsageWhenAbsent(t *testing.T) {
	req := mustParse(t, `{"model":"gpt-4o-mini","messages":[],"stream":true}`)

	body, err := buildRequestBody(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]json.RawMessage
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	var opts map[string]bool
	if err := json.Unmarshal(out["stream_options"], &opts); err != nil {
		t.Fatalf("expected stream_options to be set: %v", err)
	}
	if !opts["include_usage"] {
		t.Error("expected stream_options.include_usage=true to be injected")
	}
}

func TestBuildRequestBody_RespectsExplicitStreamOptions(t *testing.T) {
	req := mustParse(t, `{"model":"gpt-4o-mini","messages":[],"stream":true,"stream_options":{"include_usage":false}}`)

	body, err := buildRequestBody(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]json.RawMessage
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	var opts map[string]bool
	if err := json.Unmarshal(out["stream_options"], &opts); err != nil {
		t.Fatalf("expected stream_options to be present: %v", err)
	}
	if opts["include_usage"] {
		t.Error("expected the client's explicit include_usage=false to be honored, not overridden")
	}

	if requestWantsUsage(req) {
		t.Error("expected requestWantsUsage=false when the client explicitly disabled it")
	}
}

func TestBuildRequestBody_NonStreamingUnaffected(t *testing.T) {
	req := mustParse(t, `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`)

	body, err := buildRequestBody(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if _, ok := out["stream_options"]; ok {
		t.Error("did not expect stream_options on a non-streaming request")
	}
}
