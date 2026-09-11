package types

import (
	"encoding/json"
	"fmt"
)

// Message and Tool are kept as raw JSON rather than fully-typed structs.
// OpenAI message shapes vary widely by role (content as a string or an
// array of parts, tool_calls, tool_call_id, refusal, audio, ...) and
// Fluxen never needs to modify their contents — only classify a handful of
// workload-profile signals from them (see ExtractWorkloadFeatures). Storing
// them as raw JSON means the exact client-provided shape is preserved with
// zero risk of a hand-modeled struct dropping a field a future OpenAI
// release adds.
type (
	Message = json.RawMessage
	Tool    = json.RawMessage
)

// CanonicalRequest is Fluxen's provider-neutral request shape. It is
// deliberately identical in field-shape to an OpenAI chat completion
// request — that is the dialect applications already speak, and the one
// every provider adapter translates to/from (Part C.3/C.4 of the
// implementation specification).
//
// Extra holds every top-level request field Fluxen does not model
// explicitly (logprobs, presence_penalty, user, parallel_tool_calls,
// stream_options, ...). Round-tripping a CanonicalRequest through JSON()
// after Parse reproduces the original request byte-for-byte at the field
// level — new OpenAI parameters never get silently dropped just because
// Fluxen doesn't know about them yet.
type CanonicalRequest struct {
	Model          string
	Messages       []Message
	Tools          []Tool
	ToolChoice     json.RawMessage
	ResponseFormat json.RawMessage
	Temperature    *float64
	TopP           *float64
	MaxTokens      *int
	Stop           []string
	Seed           *int
	N              *int
	Stream         bool
	Extra          map[string]json.RawMessage
}

// knownRequestFields are the CanonicalRequest fields extracted out of
// Extra during parsing and re-embedded by JSON() — everything else in a
// client's request body passes through Extra untouched.
var knownRequestFields = []string{
	"model", "messages", "tools", "tool_choice", "response_format",
	"temperature", "top_p", "max_tokens", "stop", "seed", "n", "stream",
}

// ParseCanonicalRequest parses a client request body into a
// CanonicalRequest. It never fails on an unrecognized field — those are
// preserved in Extra — but does fail if the body isn't valid JSON or the
// "model" field is missing, since every downstream stage (pricing,
// routing, restriction) requires a model to reason about.
func ParseCanonicalRequest(body []byte) (*CanonicalRequest, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("types: invalid request JSON: %w", err)
	}

	req := &CanonicalRequest{Extra: map[string]json.RawMessage{}}

	if v, ok := raw["model"]; ok {
		if err := json.Unmarshal(v, &req.Model); err != nil {
			return nil, fmt.Errorf("types: invalid \"model\" field: %w", err)
		}
	}
	if req.Model == "" {
		return nil, fmt.Errorf("types: request is missing required \"model\" field")
	}

	if v, ok := raw["messages"]; ok {
		if err := json.Unmarshal(v, &req.Messages); err != nil {
			return nil, fmt.Errorf("types: invalid \"messages\" field: %w", err)
		}
	}
	if v, ok := raw["tools"]; ok {
		if err := json.Unmarshal(v, &req.Tools); err != nil {
			return nil, fmt.Errorf("types: invalid \"tools\" field: %w", err)
		}
	}
	if v, ok := raw["tool_choice"]; ok {
		req.ToolChoice = v
	}
	if v, ok := raw["response_format"]; ok {
		req.ResponseFormat = v
	}
	if v, ok := raw["temperature"]; ok {
		if err := json.Unmarshal(v, &req.Temperature); err != nil {
			return nil, fmt.Errorf("types: invalid \"temperature\" field: %w", err)
		}
	}
	if v, ok := raw["top_p"]; ok {
		if err := json.Unmarshal(v, &req.TopP); err != nil {
			return nil, fmt.Errorf("types: invalid \"top_p\" field: %w", err)
		}
	}
	if v, ok := raw["max_tokens"]; ok {
		if err := json.Unmarshal(v, &req.MaxTokens); err != nil {
			return nil, fmt.Errorf("types: invalid \"max_tokens\" field: %w", err)
		}
	}
	if v, ok := raw["stop"]; ok {
		// "stop" may be a single string or an array of strings in the
		// OpenAI dialect; normalize to []string.
		var multi []string
		if err := json.Unmarshal(v, &multi); err == nil {
			req.Stop = multi
		} else {
			var single string
			if err := json.Unmarshal(v, &single); err != nil {
				return nil, fmt.Errorf("types: invalid \"stop\" field: %w", err)
			}
			req.Stop = []string{single}
		}
	}
	if v, ok := raw["seed"]; ok {
		if err := json.Unmarshal(v, &req.Seed); err != nil {
			return nil, fmt.Errorf("types: invalid \"seed\" field: %w", err)
		}
	}
	if v, ok := raw["n"]; ok {
		if err := json.Unmarshal(v, &req.N); err != nil {
			return nil, fmt.Errorf("types: invalid \"n\" field: %w", err)
		}
	}
	if v, ok := raw["stream"]; ok {
		if err := json.Unmarshal(v, &req.Stream); err != nil {
			return nil, fmt.Errorf("types: invalid \"stream\" field: %w", err)
		}
	}

	for _, k := range knownRequestFields {
		delete(raw, k)
	}
	req.Extra = raw

	return req, nil
}

// JSON reconstructs the canonical (OpenAI-dialect) wire request. Every
// field ParseCanonicalRequest extracted is re-embedded alongside whatever
// remained in Extra, so a request that round-trips through
// Parse→JSON reproduces the original body's fields exactly — this is what
// "unknown fields pass through untouched" means in practice.
func (r *CanonicalRequest) JSON() ([]byte, error) {
	out := make(map[string]json.RawMessage, len(r.Extra)+len(knownRequestFields))
	for k, v := range r.Extra {
		out[k] = v
	}

	if err := setField(out, "model", r.Model); err != nil {
		return nil, err
	}
	if r.Messages != nil {
		if err := setField(out, "messages", r.Messages); err != nil {
			return nil, err
		}
	}
	if r.Tools != nil {
		if err := setField(out, "tools", r.Tools); err != nil {
			return nil, err
		}
	}
	if r.ToolChoice != nil {
		out["tool_choice"] = r.ToolChoice
	}
	if r.ResponseFormat != nil {
		out["response_format"] = r.ResponseFormat
	}
	if r.Temperature != nil {
		if err := setField(out, "temperature", *r.Temperature); err != nil {
			return nil, err
		}
	}
	if r.TopP != nil {
		if err := setField(out, "top_p", *r.TopP); err != nil {
			return nil, err
		}
	}
	if r.MaxTokens != nil {
		if err := setField(out, "max_tokens", *r.MaxTokens); err != nil {
			return nil, err
		}
	}
	if len(r.Stop) > 0 {
		if err := setField(out, "stop", r.Stop); err != nil {
			return nil, err
		}
	}
	if r.Seed != nil {
		if err := setField(out, "seed", *r.Seed); err != nil {
			return nil, err
		}
	}
	if r.N != nil {
		if err := setField(out, "n", *r.N); err != nil {
			return nil, err
		}
	}
	if r.Stream {
		if err := setField(out, "stream", true); err != nil {
			return nil, err
		}
	}

	return json.Marshal(out)
}

func setField(m map[string]json.RawMessage, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("types: failed to marshal %q: %w", key, err)
	}
	m[key] = b
	return nil
}
