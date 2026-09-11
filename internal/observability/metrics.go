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
// Phase 0 registers only a process-uptime gauge, per the specification's
// Phase 0 scope ("process-uptime gauge is enough for now"). Gateway
// latency, provider error rates, cache hit ratio, ingest buffer depth, and
// job health are added by the phases that produce those signals.
type Metrics struct {
	registry *prometheus.Registry
}

// NewMetrics creates a fresh Prometheus registry, registers the standard Go
// process/runtime collectors plus a fluxen_uptime_seconds gauge, and
// returns the handler for /metrics.
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

	reg.MustRegister(uptime)

	return &Metrics{registry: reg}
}

// Handler returns the HTTP handler to mount at /metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
