package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
)

// WorkloadFeatures are the request-shape signals detectors use later
// (Part G.3) without ever needing to store or re-read request bodies —
// they're computed once at ingest time and persisted as plain columns on
// requests (Part E.1).
type WorkloadFeatures struct {
	HasTools         bool
	HasToolCalls     bool
	HasImages        bool
	JSONMode         bool
	MessageCount     int
	SystemPromptHash []byte // sha256 of the first system-role message's content, nil if none
}

// messageEnvelope is the minimal shape needed to classify a message
// without modeling every possible content-part variant.
type messageEnvelope struct {
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	ToolCalls json.RawMessage `json:"tool_calls"`
}

type contentPart struct {
	Type string `json:"type"`
}

// ExtractWorkloadFeatures classifies a CanonicalRequest's shape. It never
// fails — a message it can't parse simply doesn't contribute any signal,
// since these features are inputs to advisory detectors, not to request
// validity.
func ExtractWorkloadFeatures(req *CanonicalRequest) WorkloadFeatures {
	f := WorkloadFeatures{
		HasTools:     len(req.Tools) > 0,
		MessageCount: len(req.Messages),
	}

	if req.ResponseFormat != nil {
		var rf struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(req.ResponseFormat, &rf); err == nil {
			f.JSONMode = rf.Type == "json_object" || rf.Type == "json_schema"
		}
	}

	for _, raw := range req.Messages {
		var m messageEnvelope
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}

		if m.Role == "tool" {
			f.HasToolCalls = true
		}
		if len(m.ToolCalls) > 0 && !bytes.Equal(bytes.TrimSpace(m.ToolCalls), []byte("null")) {
			f.HasToolCalls = true
		}

		if len(m.Content) > 0 {
			var parts []contentPart
			if err := json.Unmarshal(m.Content, &parts); err == nil {
				for _, p := range parts {
					if p.Type == "image_url" || p.Type == "image" {
						f.HasImages = true
					}
				}
			}
		}

		if m.Role == "system" && f.SystemPromptHash == nil {
			sum := sha256.Sum256(m.Content)
			f.SystemPromptHash = sum[:]
		}
	}

	return f
}
