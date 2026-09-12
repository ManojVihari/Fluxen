package gemini

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// fakeReadCloser adapts a strings.Reader to io.ReadCloser for streamReader's body field.
type fakeReadCloser struct{ io.Reader }

func (f fakeReadCloser) Close() error { return nil }

func newTestStreamReader(sse string) *streamReader {
	r := strings.NewReader(sse)
	return &streamReader{body: fakeReadCloser{r}, reader: bufio.NewReader(r), id: "chatcmpl-test", model: "gemini-1.5-flash"}
}

func TestStreamReader_TranslatesTextDeltasAndUsage(t *testing.T) {
	sse := "" +
		"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"Hel\"}]},\"index\":0}]}\n\n" +
		"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"lo!\"}]},\"finishReason\":\"STOP\",\"index\":0}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":2,\"totalTokenCount\":7}}\n\n"

	s := newTestStreamReader(sse)

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
		var parsed map[string]json.RawMessage
		payload := strings.TrimPrefix(strings.TrimSpace(string(chunk)), "data: ")
		if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
			t.Fatalf("chunk is not valid JSON: %v (%s)", err, chunk)
		}
		var choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		}
		if err := json.Unmarshal(parsed["choices"], &choices); err != nil {
			t.Fatalf("failed to parse choices: %v", err)
		}
		if len(choices) > 0 && choices[0].Delta.Content != "" {
			texts = append(texts, choices[0].Delta.Content)
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
		t.Errorf("expected usage 5/2 from the final chunk, got %+v", s.Usage())
	}
}
