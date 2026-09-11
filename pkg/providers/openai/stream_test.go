package openai

import (
	"bufio"
	"bytes"
	"io"
	"strings"
	"testing"
)

func newTestStreamReader(sse string) *streamReader {
	rc := io.NopCloser(strings.NewReader(sse))
	return &streamReader{body: rc, reader: bufio.NewReader(rc), usageSource: true}
}

func TestStreamReader_ForwardsChunksVerbatimAndCapturesUsage(t *testing.T) {
	sse := "data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n" +
		"data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n" +
		"data: {\"id\":\"1\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}\n\n" +
		"data: [DONE]\n\n"

	sr := newTestStreamReader(sse)

	var chunks [][]byte
	for {
		c, err := sr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Copy since the buffer may be reused by the caller pattern.
		cp := append([]byte(nil), c...)
		chunks = append(chunks, cp)
	}

	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks (3 data + [DONE]), got %d", len(chunks))
	}
	if !bytes.Contains(chunks[0], []byte(`"content":"Hel"`)) {
		t.Errorf("expected first chunk to contain the first delta verbatim, got %s", chunks[0])
	}
	if !bytes.HasSuffix(chunks[0], []byte("\n\n")) {
		t.Errorf("expected each chunk to preserve SSE framing (trailing blank line), got %q", chunks[0])
	}
	if !bytes.Contains(chunks[3], []byte("[DONE]")) {
		t.Errorf("expected the final chunk to be the [DONE] event, got %s", chunks[3])
	}

	usage := sr.Usage()
	if usage.InputTokens != 10 || usage.OutputTokens != 2 || usage.TotalTokens != 12 {
		t.Errorf("expected usage to be captured from the penultimate chunk, got %+v", usage)
	}
}

func TestStreamReader_EOFAfterDone(t *testing.T) {
	sr := newTestStreamReader("data: [DONE]\n\n")

	_, err := sr.Next()
	if err != nil {
		t.Fatalf("unexpected error on first Next(): %v", err)
	}
	_, err = sr.Next()
	if err != io.EOF {
		t.Fatalf("expected io.EOF after [DONE], got %v", err)
	}
}

func TestStreamReader_TruncatedStreamStillForwardsPartialChunk(t *testing.T) {
	// No trailing blank line — simulates an upstream connection that
	// closes mid-event.
	sr := newTestStreamReader("data: {\"id\":\"1\"}")

	chunk, err := sr.Next()
	if err != nil {
		t.Fatalf("expected the partial chunk to be returned without error, got: %v", err)
	}
	if !bytes.Contains(chunk, []byte(`"id":"1"`)) {
		t.Errorf("expected partial chunk content to be forwarded, got %s", chunk)
	}

	_, err = sr.Next()
	if err != io.EOF {
		t.Fatalf("expected io.EOF on the following call, got %v", err)
	}
}
