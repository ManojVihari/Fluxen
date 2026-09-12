package gemini

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"fluxen/pkg/types"
)

// openAIMessage is the subset of an OpenAI chat message this package
// reads. Content is left raw because it can be either a plain string or
// an array of typed parts (text/image_url) — decodeContent below handles
// both.
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

type openAITool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	} `json:"function"`
}

// geminiPart is one part of a Gemini content turn — a tagged union in
// Gemini's own wire format (exactly one of these fields set per part).
type geminiPart struct {
	Text             string              `json:"text,omitempty"`
	InlineData       *geminiInlineData   `json:"inlineData,omitempty"`
	FunctionCall     *geminiFunctionCall `json:"functionCall,omitempty"`
	FunctionResponse *geminiFuncResponse `json:"functionResponse,omitempty"`
}

type geminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiFunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type geminiFuncResponse struct {
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiGenerationConfig struct {
	Temperature      *float64        `json:"temperature,omitempty"`
	TopP             *float64        `json:"topP,omitempty"`
	MaxOutputTokens  *int            `json:"maxOutputTokens,omitempty"`
	StopSequences    []string        `json:"stopSequences,omitempty"`
	ResponseMimeType string          `json:"responseMimeType,omitempty"`
	ResponseSchema   json.RawMessage `json:"responseSchema,omitempty"`
}

type geminiFunctionDeclaration struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations"`
}

type geminiRequest struct {
	Contents          []geminiContent         `json:"contents"`
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
	Tools             []geminiTool            `json:"tools,omitempty"`
}

// buildGeminiRequest translates a CanonicalRequest (OpenAI dialect) into
// a Gemini generateContent request body. It tracks each assistant
// tool_call's id -> function name so a later "tool" role message (which
// OpenAI only tags with tool_call_id, not the function name) can be
// translated into Gemini's functionResponse part, which requires the
// name.
func buildGeminiRequest(req *types.CanonicalRequest) ([]byte, error) {
	toolCallNames := map[string]string{}

	var systemParts []geminiPart
	var contents []geminiContent

	for _, raw := range req.Messages {
		var m openAIMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("gemini: failed to parse message: %w", err)
		}

		switch m.Role {
		case "system", "developer":
			parts, err := decodeContentParts(m.Content)
			if err != nil {
				return nil, err
			}
			systemParts = append(systemParts, parts...)

		case "user":
			parts, err := decodeContentParts(m.Content)
			if err != nil {
				return nil, err
			}
			contents = append(contents, geminiContent{Role: "user", Parts: parts})

		case "assistant":
			var parts []geminiPart
			if len(m.Content) > 0 {
				p, err := decodeContentParts(m.Content)
				if err != nil {
					return nil, err
				}
				parts = append(parts, p...)
			}
			for _, tc := range m.ToolCalls {
				toolCallNames[tc.ID] = tc.Function.Name
				parts = append(parts, geminiPart{FunctionCall: &geminiFunctionCall{
					Name: tc.Function.Name, Args: json.RawMessage(orEmptyObject(tc.Function.Arguments)),
				}})
			}
			contents = append(contents, geminiContent{Role: "model", Parts: parts})

		case "tool":
			name := toolCallNames[m.ToolCallID]
			var text string
			_ = json.Unmarshal(m.Content, &text)
			if text == "" {
				text = string(m.Content)
			}
			contents = append(contents, geminiContent{Role: "user", Parts: []geminiPart{{
				FunctionResponse: &geminiFuncResponse{Name: name, Response: json.RawMessage(fmt.Sprintf(`{"content":%s}`, mustJSONString(text)))},
			}}})
		}
	}

	out := geminiRequest{Contents: contents}
	if len(systemParts) > 0 {
		out.SystemInstruction = &geminiContent{Parts: systemParts}
	}

	genConfig := &geminiGenerationConfig{}
	hasGenConfig := false
	if req.Temperature != nil {
		genConfig.Temperature = req.Temperature
		hasGenConfig = true
	}
	if req.TopP != nil {
		genConfig.TopP = req.TopP
		hasGenConfig = true
	}
	if req.MaxTokens != nil {
		genConfig.MaxOutputTokens = req.MaxTokens
		hasGenConfig = true
	}
	if len(req.Stop) > 0 {
		genConfig.StopSequences = req.Stop
		hasGenConfig = true
	}
	if mimeType, schema, ok := decodeResponseFormat(req.ResponseFormat); ok {
		genConfig.ResponseMimeType = mimeType
		genConfig.ResponseSchema = schema
		hasGenConfig = true
	}
	if hasGenConfig {
		out.GenerationConfig = genConfig
	}

	if len(req.Tools) > 0 {
		var decls []geminiFunctionDeclaration
		for _, raw := range req.Tools {
			var t openAITool
			if err := json.Unmarshal(raw, &t); err != nil {
				continue // a malformed tool entry is skipped, not fatal to the whole request
			}
			if t.Type != "" && t.Type != "function" {
				continue
			}
			decls = append(decls, geminiFunctionDeclaration{
				Name: t.Function.Name, Description: t.Function.Description, Parameters: t.Function.Parameters,
			})
		}
		if len(decls) > 0 {
			out.Tools = []geminiTool{{FunctionDeclarations: decls}}
		}
	}

	return json.Marshal(out)
}

// decodeContentParts handles both OpenAI content shapes: a plain string,
// or an array of typed parts (text / image_url). A data: URI image is
// translated to Gemini's inlineData; a remote http(s) image URL has no
// direct Gemini equivalent without Fluxen fetching it server-side, which
// this pass does not do — it is dropped with the rest of the message's
// parts preserved, a documented limitation (Part D's table already marks
// Gemini vision as "via translation," not "identical").
func decodeContentParts(raw json.RawMessage) ([]geminiPart, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		if asString == "" {
			return nil, nil
		}
		return []geminiPart{{Text: asString}}, nil
	}

	var asParts []openAIContentPart
	if err := json.Unmarshal(raw, &asParts); err != nil {
		return nil, fmt.Errorf("gemini: unrecognized message content shape: %w", err)
	}

	var parts []geminiPart
	for _, p := range asParts {
		switch p.Type {
		case "text":
			if p.Text != "" {
				parts = append(parts, geminiPart{Text: p.Text})
			}
		case "image_url":
			if mimeType, data, ok := decodeDataURI(p.ImageURL.URL); ok {
				parts = append(parts, geminiPart{InlineData: &geminiInlineData{MimeType: mimeType, Data: data}})
			}
			// Remote (non-data:) URLs are intentionally dropped -- see doc
			// comment above.
		}
	}
	return parts, nil
}

// decodeDataURI splits a "data:<mime>;base64,<data>" URI into its mime
// type and raw base64 payload (Gemini's inlineData wants exactly this
// split, unlike OpenAI's single combined data: URI string).
func decodeDataURI(uri string) (mimeType, data string, ok bool) {
	const prefix = "data:"
	if !strings.HasPrefix(uri, prefix) {
		return "", "", false
	}
	rest := uri[len(prefix):]
	comma := strings.IndexByte(rest, ',')
	if comma < 0 {
		return "", "", false
	}
	meta, payload := rest[:comma], rest[comma+1:]
	if !strings.HasSuffix(meta, ";base64") {
		return "", "", false
	}
	mimeType = strings.TrimSuffix(meta, ";base64")
	if _, err := base64.StdEncoding.DecodeString(payload); err != nil {
		return "", "", false
	}
	return mimeType, payload, true
}

// decodeResponseFormat translates OpenAI's response_format into Gemini's
// responseMimeType/responseSchema pair. json_object has no schema;
// json_schema carries one (Part D: "translated (responseSchema)").
func decodeResponseFormat(raw json.RawMessage) (mimeType string, schema json.RawMessage, ok bool) {
	if len(raw) == 0 {
		return "", nil, false
	}
	var rf struct {
		Type       string `json:"type"`
		JSONSchema struct {
			Schema json.RawMessage `json:"schema"`
		} `json:"json_schema"`
	}
	if err := json.Unmarshal(raw, &rf); err != nil {
		return "", nil, false
	}
	switch rf.Type {
	case "json_object":
		return "application/json", nil, true
	case "json_schema":
		return "application/json", rf.JSONSchema.Schema, true
	default:
		return "", nil, false
	}
}

func orEmptyObject(s string) string {
	if strings.TrimSpace(s) == "" {
		return "{}"
	}
	return s
}

func mustJSONString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}
