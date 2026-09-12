package ollama

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

type fakeReadCloser struct{ io.Reader }

func (f fakeReadCloser) Close() error { return nil }

func newTestStreamReader(ndjson string) *streamReader {
	r := strings.NewReader(ndjson)
	return &streamReader{body: fakeReadCloser{r}, reader: bufio.NewReader(r), id: "chatcmpl-test", model: "llama3.2"}
}

func TestStreamReader_TranslatesNDJSONDeltasAndUsage(t *testing.T) {
	ndjson := "" +
		`{"model":"llama3.2","message":{"role":"assistant","content":"Hel"},"done":false}` + "\n" +
		`{"model":"llama3.2","message":{"role":"assistant","content":"lo!"},"done":false}` + "\n" +
		`{"model":"llama3.2","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":5,"eval_count":2}` + "\n"

	s := newTestStreamReader(ndjson)

	var texts []string
	var sawDone bool
	for {
		chunk, err := s.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(string(chunk), "[DONE]") {
			sawDone = true
			continue
		}
		payload := strings.TrimPrefix(strings.TrimSpace(string(chunk)), "data: ")
		var parsed struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
			t.Fatalf("chunk is not valid JSON: %v (%s)", err, chunk)
		}
		if len(parsed.Choices) > 0 && parsed.Choices[0].Delta.Content != "" {
			texts = append(texts, parsed.Choices[0].Delta.Content)
		}
	}

	if !sawDone {
		t.Error("expected a final [DONE] event")
	}
	got := strings.Join(texts, "")
	if got != "Hello!" {
		t.Errorf("expected translated deltas to spell Hello!, got %q", got)
	}
	if s.Usage().InputTokens != 5 || s.Usage().OutputTokens != 2 {
		t.Errorf("expected usage 5/2 from the final NDJSON line, got %+v", s.Usage())
	}
}
