package ollama

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"fluxen/pkg/types"
)

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    json.RawMessage  `json:"content"`
	Name       string           `json:"name,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

// ollamaMessage mirrors /api/chat's native message shape: content is
// always a plain string (unlike OpenAI's string-or-parts-array), and
// images are a separate list of raw base64 strings with no data: URI
// wrapper or mime type.
type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	Images    []string         `json:"images,omitempty"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaToolCall struct {
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type ollamaOptions struct {
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	NumPredict  *int     `json:"num_predict,omitempty"`
	Stop        []string `json:"stop,omitempty"`
	Seed        *int     `json:"seed,omitempty"`
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	// Tools is passed through verbatim (Part D: "model-dependent, passed
	// through untranslated") — Ollama's own /api/chat tool schema is
	// already OpenAI-shaped for models that support it, so no translation
	// is attempted; a model that doesn't support tools simply ignores the
	// field or Ollama itself rejects the request, exactly as it would
	// without Fluxen in the path.
	Tools   []json.RawMessage `json:"tools,omitempty"`
	Format  json.RawMessage   `json:"format,omitempty"`
	Options *ollamaOptions    `json:"options,omitempty"`
}

// buildOllamaRequest translates a CanonicalRequest (OpenAI dialect) into
// an Ollama /api/chat request body.
func buildOllamaRequest(req *types.CanonicalRequest, stream bool) ([]byte, error) {
	messages := make([]ollamaMessage, 0, len(req.Messages))
	for _, raw := range req.Messages {
		var m openAIMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}

		om := ollamaMessage{Role: m.Role}
		text, images, err := decodeContent(m.Content)
		if err != nil {
			return nil, err
		}
		om.Content, om.Images = text, images

		for _, tc := range m.ToolCalls {
			var otc ollamaToolCall
			otc.Function.Name = tc.Function.Name
			args := tc.Function.Arguments
			if strings.TrimSpace(args) == "" {
				args = "{}"
			}
			otc.Function.Arguments = json.RawMessage(args)
			om.ToolCalls = append(om.ToolCalls, otc)
		}

		messages = append(messages, om)
	}

	out := ollamaRequest{Model: req.Model, Messages: messages, Stream: stream}

	if len(req.Tools) > 0 {
		out.Tools = req.Tools
	}
	if mimeType, ok := decodeResponseFormat(req.ResponseFormat); ok {
		out.Format = json.RawMessage(mimeType)
	}

	opts := &ollamaOptions{}
	hasOpts := false
	if req.Temperature != nil {
		opts.Temperature = req.Temperature
		hasOpts = true
	}
	if req.TopP != nil {
		opts.TopP = req.TopP
		hasOpts = true
	}
	if req.MaxTokens != nil {
		opts.NumPredict = req.MaxTokens
		hasOpts = true
	}
	if len(req.Stop) > 0 {
		opts.Stop = req.Stop
		hasOpts = true
	}
	if req.Seed != nil {
		opts.Seed = req.Seed
		hasOpts = true
	}
	if hasOpts {
		out.Options = opts
	}

	return json.Marshal(out)
}

// decodeContent flattens OpenAI's string-or-parts content into Ollama's
// plain-string content plus a separate images list. Only data: URI
// images translate directly (Ollama's images field wants raw base64, no
// wrapper) — a remote http(s) image URL is dropped, the same documented
// limitation pkg/providers/gemini has, since fetching an arbitrary URL
// server-side is out of scope for this pass.
func decodeContent(raw json.RawMessage) (text string, images []string, err error) {
	if len(raw) == 0 {
		return "", nil, nil
	}

	var asString string
	if unmarshalErr := json.Unmarshal(raw, &asString); unmarshalErr == nil {
		return asString, nil, nil
	}

	var asParts []openAIContentPart
	if unmarshalErr := json.Unmarshal(raw, &asParts); unmarshalErr != nil {
		return "", nil, unmarshalErr
	}

	var sb strings.Builder
	for _, p := range asParts {
		switch p.Type {
		case "text":
			sb.WriteString(p.Text)
		case "image_url":
			if data, ok := decodeDataURIPayload(p.ImageURL.URL); ok {
				images = append(images, data)
			}
		}
	}
	return sb.String(), images, nil
}

// decodeDataURIPayload extracts just the base64 payload from a
// "data:<mime>;base64,<data>" URI — Ollama's images field wants the
// payload alone, unlike Gemini's inlineData which keeps the mime type
// alongside it.
func decodeDataURIPayload(uri string) (string, bool) {
	const prefix = "data:"
	if !strings.HasPrefix(uri, prefix) {
		return "", false
	}
	comma := strings.IndexByte(uri, ',')
	if comma < 0 {
		return "", false
	}
	meta, payload := uri[len(prefix):comma], uri[comma+1:]
	if !strings.HasSuffix(meta, ";base64") {
		return "", false
	}
	if _, err := base64.StdEncoding.DecodeString(payload); err != nil {
		return "", false
	}
	return payload, true
}

// decodeResponseFormat maps OpenAI's response_format onto Ollama's much
// simpler `format` field: "json" for both json_object and json_schema
// (Part D: "best-effort, not guaranteed enforced" — Ollama has no schema
// parameter to translate a json_schema's actual schema into).
func decodeResponseFormat(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var rf struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &rf); err != nil {
		return "", false
	}
	switch rf.Type {
	case "json_object", "json_schema":
		return `"json"`, true
	default:
		return "", false
	}
}
