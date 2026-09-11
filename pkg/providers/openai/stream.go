package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"fluxen/pkg/providers"
	"fluxen/pkg/types"
)

// ChatStream performs a streaming chat completion call. The returned
// StreamReader yields each SSE event's raw bytes exactly as received —
// Fluxen never reconstructs a streamed chunk, only reads a copy of it to
// extract usage (Part C.6: "Tee, don't buffer").
func (c *Client) ChatStream(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (providers.StreamReader, error) {
	body, err := buildRequestBody(req)
	if err != nil {
		return nil, err
	}
	wantsUsage := requestWantsUsage(req)

	httpReq, err := newRequest(ctx, http.MethodPost, baseURL(cred)+"/v1/chat/completions", bytes.NewReader(body), cred)
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
		return nil, &providers.UpstreamError{StatusCode: httpResp.StatusCode, Body: respBody}
	}

	return &streamReader{
		body:        httpResp.Body,
		reader:      bufio.NewReader(httpResp.Body),
		usageSource: wantsUsage,
	}, nil
}

// streamReader implements providers.StreamReader over an OpenAI SSE
// response body. Each SSE event is "data: <json>\n\n" (or "data:
// [DONE]\n\n" to terminate); Next() returns the exact bytes of one event,
// unmodified, while separately parsing a copy of the JSON payload to
// capture the usage object OpenAI attaches to the final content chunk when
// stream_options.include_usage is set.
type streamReader struct {
	body   io.ReadCloser
	reader *bufio.Reader

	usageSource bool // whether we asked OpenAI for a final usage chunk
	usage       types.ResponseUsage
	done        bool
}

// Next reads lines until it has one complete SSE event: one or more
// non-blank lines followed by a blank line, per the SSE spec. A leading
// run of blank lines (before any content) is skipped rather than treated
// as an empty event.
func (s *streamReader) Next() ([]byte, error) {
	if s.done {
		return nil, io.EOF
	}

	var event bytes.Buffer
	for {
		line, readErr := s.reader.ReadBytes('\n')
		isBlank := len(bytes.TrimRight(line, "\r\n")) == 0

		if len(line) > 0 && !(isBlank && event.Len() == 0) {
			event.Write(line)
		}

		if isBlank && event.Len() > 0 {
			// Terminator reached with real content collected.
			readErr = nil
			break
		}

		if readErr != nil {
			s.done = true
			if readErr == io.EOF {
				if event.Len() == 0 {
					return nil, io.EOF
				}
				break // upstream closed without a trailing blank line
			}
			return nil, readErr
		}
	}

	chunk := event.Bytes()
	s.inspect(chunk)
	if bytes.Contains(chunk, []byte("data: [DONE]")) {
		s.done = true
	}
	return chunk, nil
}

// inspect parses one SSE event's JSON payload (if any) to capture usage,
// without altering the bytes that get forwarded to the client.
func (s *streamReader) inspect(chunk []byte) {
	const prefix = "data:"
	for _, line := range bytes.Split(chunk, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		if !bytes.HasPrefix(line, []byte(prefix)) {
			continue
		}
		payload := bytes.TrimSpace(line[len(prefix):])
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}

		var env struct {
			Usage *openAIUsage `json:"usage"`
		}
		if err := json.Unmarshal(payload, &env); err != nil {
			continue
		}
		if env.Usage != nil {
			s.usage = types.ResponseUsage{
				InputTokens:       env.Usage.PromptTokens,
				OutputTokens:      env.Usage.CompletionTokens,
				TotalTokens:       env.Usage.TotalTokens,
				CachedInputTokens: env.Usage.PromptTokensDetails.CachedTokens,
				ReasoningTokens:   env.Usage.CompletionTokensDetails.ReasoningTokens,
			}
		}
	}
}

func (s *streamReader) Usage() types.ResponseUsage { return s.usage }

func (s *streamReader) Close() error { return s.body.Close() }
