package observability

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds the Prometheus registry for a Fluxen process. It is
// constructed once at boot and its Handler is mounted at /metrics.
//
// Phase 0 registered only a process-uptime gauge. Phase 1 adds
// fluxen_usage_dropped_total — the counter the hot-path contract (Part
// B.2) requires: when the ingest queue is full, a usage record is dropped
// rather than blocking a proxied request, and this counter is what makes
// that tradeoff observable instead of silent.
type Metrics struct {
	registry *prometheus.Registry

	UsageDropped prometheus.Counter
}

// NewMetrics creates a fresh Prometheus registry, registers the standard Go
// process/runtime collectors plus fluxen_uptime_seconds and
// fluxen_usage_dropped_total, and returns the handler for /metrics.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()

	start := time.Now()
	uptime := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "fluxen_uptime_seconds",
			Help: "Seconds since this Fluxen process started.",
		},
		func() float64 { return time.Since(start).Seconds() },
	)

	usageDropped := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "fluxen_usage_dropped_total",
		Help: "Usage records dropped because the ingest queue was full. Serving traffic always wins over recording it (Part B.2).",
	})

	reg.MustRegister(uptime, usageDropped)

	return &Metrics{registry: reg, UsageDropped: usageDropped}
}

// Handler returns the HTTP handler to mount at /metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
