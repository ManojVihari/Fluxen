package ollama

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

// ChatStream performs a streaming /api/chat call. Ollama's stream is
// newline-delimited JSON (one full JSON object per line, no "data:"
// framing) — genuinely different from both OpenAI's and Gemini's SSE
// framing (Part D: providers are not pretended to be identical). The
// returned StreamReader yields OpenAI-shaped chat.completion.chunk SSE
// events synthesized from each line, translated and forwarded as soon as
// it arrives (Part C.6: never buffer the full body).
func (c *Client) ChatStream(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (providers.StreamReader, error) {
	body, err := buildOllamaRequest(req, true)
	if err != nil {
		return nil, err
	}

	httpReq, err := newRequest(ctx, http.MethodPost, baseURL(cred)+"/api/chat", bytes.NewReader(body), cred)
	if err != nil {
		return nil, err
	}

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

// Next returns the next translated OpenAI SSE event: one Ollama NDJSON
// line in, one chat.completion.chunk SSE event out, then a synthesized
// "data: [DONE]\n\n" once the NDJSON stream ends (Ollama's stream has no
// [DONE] sentinel of its own — its final line simply carries done:true).
func (s *streamReader) Next() ([]byte, error) {
	for !s.upstreamDone {
		line, err := s.reader.ReadBytes('\n')
		line = bytes.TrimSpace(line)
		if err != nil && err != io.EOF {
			return nil, err
		}
		if err == io.EOF {
			s.upstreamDone = true
		}
		if len(line) == 0 {
			if s.upstreamDone {
				break
			}
			continue
		}

		chunk, done, ok := s.translateLine(line)
		if done {
			s.upstreamDone = true
		}
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

// translateLine parses one Ollama NDJSON line and builds the equivalent
// OpenAI chat.completion.chunk SSE bytes. done reports whether this was
// Ollama's final line (done:true, carrying usage counts).
func (s *streamReader) translateLine(line []byte) (chunk []byte, done, ok bool) {
	var o ollamaResponse
	if err := json.Unmarshal(line, &o); err != nil {
		return nil, false, false
	}

	if o.Done {
		s.usage = types.ResponseUsage{
			InputTokens: o.PromptEvalCount, OutputTokens: o.EvalCount, TotalTokens: o.PromptEvalCount + o.EvalCount,
		}
	}

	var toolCalls []openAIRespToolCall
	for i, tc := range o.Message.ToolCalls {
		args := tc.Function.Arguments
		if len(args) == 0 {
			args = json.RawMessage("{}")
		}
		respTC := openAIRespToolCall{ID: fmt.Sprintf("call_%s_%d", randomID(), i)}
		respTC.Type = "function"
		respTC.Function.Name = tc.Function.Name
		respTC.Function.Arguments = string(args)
		toolCalls = append(toolCalls, respTC)
	}

	delta := map[string]any{"role": "assistant"}
	if o.Message.Content != "" {
		delta["content"] = o.Message.Content
	}
	if len(toolCalls) > 0 {
		delta["tool_calls"] = toolCalls
	}

	var finishReason *string
	if o.Done {
		fr := finishReasonFromOllama(o.DoneReason, len(toolCalls) > 0)
		finishReason = &fr
	}

	out := map[string]any{
		"id": s.id, "object": "chat.completion.chunk", "model": s.model,
		"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finishReason}},
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, o.Done, false
	}
	return append(append([]byte("data: "), b...), []byte("\n\n")...), o.Done, true
}

func (s *streamReader) Usage() types.ResponseUsage { return s.usage }

func (s *streamReader) Close() error { return s.body.Close() }
