package providers

import "fmt"

// UpstreamError wraps a non-2xx response from a provider. Body is the
// provider's own error response bytes — for Phase 1 (client dialect ==
// OpenAI == the only provider dialect) the gateway relays Body to the
// client verbatim rather than re-encoding it, which is "error
// normalization" in the trivial case where no translation is actually
// needed. Providers with a genuinely different error dialect (Gemini,
// Ollama — Phase 7) will translate Body into the client's dialect before
// relaying it.
type UpstreamError struct {
	StatusCode int
	Body       []byte
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("provider returned status %d: %s", e.StatusCode, e.Body)
}
