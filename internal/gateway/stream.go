package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"fluxen/pkg/providers"
	"fluxen/pkg/types"
)

// handleChatStream implements Part C.6's streaming contract: tee the
// upstream stream to the client immediately, flush after every chunk,
// never buffer the full body, and still produce a UsageRecord on every
// exit path — including a client disconnect, which still cost real money
// upstream. Since Phase 5, it also accumulates the raw chunks so a
// successful response can populate the cache verbatim (internal/cache's
// SSE replay needs the exact bytes, not a re-synthesized stream).
func (s *Server) handleChatStream(ctx context.Context, w http.ResponseWriter, r *http.Request, req *types.CanonicalRequest, rb *recordBuilder, provider providers.Provider, cred providers.Credential) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		// Should be unreachable behind net/http's standard server, but
		// fail cleanly rather than silently buffering the whole stream.
		writeError(w, r, http.StatusInternalServerError, "streaming_unsupported", "This server does not support streaming responses.")
		return
	}

	sr, err := provider.ChatStream(ctx, req, cred)
	if err != nil {
		s.handleProviderError(w, r, rb, err)
		return
	}
	defer sr.Close()

	w.Header().Set(RequestIDHeader, rb.requestID)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	var ttft *int
	firstChunk := true
	status := "ok"
	var errCode, errMsg string
	httpStatus := http.StatusOK

	cacheable := s.Cache != nil && rb.policyDoc.Caching != nil && rb.policyDoc.Caching.Enabled
	var chunks [][]byte

	for {
		chunk, err := sr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			status = "provider_error"
			errCode = "provider_error"
			errMsg = err.Error()
			httpStatus = http.StatusBadGateway
			break
		}

		if _, writeErr := w.Write(chunk); writeErr != nil {
			// The client went away mid-stream. Stop forwarding, but still
			// account for what OpenAI already billed us for.
			status = "client_abort"
			errCode = "client_abort"
			errMsg = writeErr.Error()
			httpStatus = 0
			break
		}
		flusher.Flush()
		if cacheable {
			chunks = append(chunks, chunk)
		}

		if firstChunk {
			ms := int(time.Since(rb.startedAt) / time.Millisecond)
			ttft = &ms
			firstChunk = false
		}

		select {
		case <-r.Context().Done():
			status = "client_abort"
			errCode = "client_abort"
			errMsg = "client disconnected"
			httpStatus = 0
			goto done
		default:
		}
	}

done:
	if errors.Is(ctx.Err(), context.DeadlineExceeded) && status == "ok" {
		status = "timeout"
		errCode = "upstream_timeout"
		errMsg = "upstream did not complete before the timeout"
		httpStatus = http.StatusGatewayTimeout
	}

	// req.Model is the actually-routed model (Phase 5 may have rerouted
	// it away from rb.requestedModel) — pricing must reflect what was
	// really called, exactly like the non-streaming path already does
	// via resp.Model. responseBody is always nil here — a streamed
	// response's SSE bytes aren't valid JSON for the jsonb response_body
	// column (see recordBuilder's own doc comment).
	rec := rb.finalize(s.Catalog, sr.Usage(), req.Model, status, httpStatus, errCode, errMsg, ttft, nil)
	s.emit(rec)
	if status == "ok" {
		s.afterSuccess(ctx, rb, rec, nil, chunks)
	}
}
