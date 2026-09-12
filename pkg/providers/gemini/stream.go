package gemini

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"fluxen/pkg/providers"
	"fluxen/pkg/types"
)

// ChatStream performs a streaming generateContent call (alt=sse). The
// returned StreamReader yields OpenAI-shaped chat.completion.chunk SSE
// events synthesized from each Gemini partial response — never Gemini's
// own bytes, since the client only ever speaks OpenAI's dialect (Part
// A.3). This is a real per-chunk translation, not a passthrough tee, but
// it still honors Part C.6's "never buffer the full body": each upstream
// SSE event is translated and forwarded as soon as it arrives.
func (c *Client) ChatStream(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (providers.StreamReader, error) {
	body, err := buildGeminiRequest(req)
	if err != nil {
		return nil, err
	}

	endpoint := baseURL(cred) + "/v1beta/models/" + req.Model + ":streamGenerateContent?alt=sse"
	httpReq, err := newRequest(ctx, http.MethodPost, endpoint, bytes.NewReader(body), cred)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "text/event-stream")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, translateError(httpResp.StatusCode, respBody)
	}

	return &streamReader{
		body: httpResp.Body, reader: bufio.NewReader(httpResp.Body),
		id: "chatcmpl-" + randomID(), model: req.Model,
	}, nil
}

type streamReader struct {
	body   io.ReadCloser
	reader *bufio.Reader

	id    string
	model string

	usage        types.ResponseUsage
	upstreamDone bool
	sentDone     bool
}

// Next returns the next translated OpenAI SSE event. It first drains
// real content from upstream (one Gemini SSE event -> one OpenAI chunk),
// then emits exactly one "data: [DONE]\n\n" once upstream closes, then
// io.EOF on every call after that.
func (s *streamReader) Next() ([]byte, error) {
	for !s.upstreamDone {
		event, err := s.readUpstreamEvent()
		if err != nil {
			if err == io.EOF {
				s.upstreamDone = true
				break
			}
			return nil, err
		}
		if len(event) == 0 {
			continue
		}
		chunk, ok := s.translateEvent(event)
		if !ok {
			continue
		}
		return chunk, nil
	}

	if !s.sentDone {
		s.sentDone = true
		return []byte("data: [DONE]\n\n"), nil
	}
	return nil, io.EOF
}

// readUpstreamEvent reads one complete "data: <json>\n\n" SSE event from
// Gemini, the same line-accumulation approach pkg/providers/openai's
// streamReader uses.
func (s *streamReader) readUpstreamEvent() ([]byte, error) {
	var event bytes.Buffer
	for {
		line, readErr := s.reader.ReadBytes('\n')
		isBlank := len(bytes.TrimRight(line, "\r\n")) == 0

		if len(line) > 0 && !(isBlank && event.Len() == 0) {
			event.Write(line)
		}
		if isBlank && event.Len() > 0 {
			return event.Bytes(), nil
		}
		if readErr != nil {
			if readErr == io.EOF && event.Len() > 0 {
				return event.Bytes(), nil
			}
			return nil, readErr
		}
	}
}

// translateEvent parses one Gemini SSE event's JSON payload and builds
// the equivalent OpenAI chat.completion.chunk SSE bytes. ok is false for
// an event that carries no translatable payload (e.g. a bare keep-alive).
func (s *streamReader) translateEvent(event []byte) ([]byte, bool) {
	const prefix = "data:"
	var payload []byte
	for _, line := range bytes.Split(event, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		if bytes.HasPrefix(line, []byte(prefix)) {
			payload = bytes.TrimSpace(line[len(prefix):])
			break
		}
	}
	if len(payload) == 0 {
		return nil, false
	}

	var g geminiResponse
	if err := json.Unmarshal(payload, &g); err != nil {
		return nil, false
	}
	if g.UsageMetadata != nil {
		s.usage = types.ResponseUsage{
			InputTokens: g.UsageMetadata.PromptTokenCount, OutputTokens: g.UsageMetadata.CandidatesTokenCount,
			TotalTokens: g.UsageMetadata.TotalTokenCount,
		}
	}
	if len(g.Candidates) == 0 {
		return nil, false
	}

	cand := g.Candidates[0]
	var deltaText string
	var toolCalls []openAIRespToolCall
	for i, part := range cand.Content.Parts {
		if part.Text != "" {
			deltaText += part.Text
		}
		if part.FunctionCall != nil {
			args := part.FunctionCall.Args
			if len(args) == 0 {
				args = json.RawMessage("{}")
			}
			tc := openAIRespToolCall{ID: fmt.Sprintf("call_%s_%d", randomID(), i)}
			tc.Type = "function"
			tc.Function.Name = part.FunctionCall.Name
			tc.Function.Arguments = string(args)
			toolCalls = append(toolCalls, tc)
		}
	}

	var finishReason *string
	if cand.FinishReason != "" {
		fr := finishReasonFromGemini(cand.FinishReason, len(toolCalls) > 0)
		finishReason = &fr
	}

	delta := map[string]any{"role": "assistant"}
	if deltaText != "" {
		delta["content"] = deltaText
	}
	if len(toolCalls) > 0 {
		delta["tool_calls"] = toolCalls
	}

	chunk := map[string]any{
		"id": s.id, "object": "chat.completion.chunk", "model": s.model,
		"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finishReason}},
	}
	b, err := json.Marshal(chunk)
	if err != nil {
		return nil, false
	}
	return append(append([]byte("data: "), b...), []byte("\n\n")...), true
}

func (s *streamReader) Usage() types.ResponseUsage { return s.usage }

func (s *streamReader) Close() error { return s.body.Close() }
