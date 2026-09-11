// Package observability provides the structured logging and metrics
// primitives shared by every Fluxen binary. It deliberately stays thin in
// Phase 0: a JSON slog logger and a Prometheus registry with a process
// uptime gauge. Request-scoped fields, provider/cache/job metrics, and log
// redaction are added by the phases that produce the data they describe.
package observability

import (
	"log/slog"
	"os"
	"strings"
)

// NewLogger returns a structured JSON logger writing to stdout, which is
// the expected log destination for a container running under Docker
// Compose or any process supervisor. level is one of "debug", "info",
// "warn", "error" (case-insensitive); an unrecognized value falls back to
// "info" rather than failing boot over a logging preference.
func NewLogger(level string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(level),
	})
	return slog.New(handler)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
