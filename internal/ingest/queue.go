// Package ingest is the gateway's async path from a completed request to a
// durable row in Postgres. It exists entirely to satisfy the hot-path
// contract (Part B.2 of the implementation specification): a proxied
// request must never fail, stall, or slow down because analytics
// persistence is slow or unavailable. Enqueue is therefore always
// non-blocking — if the queue is full, the record is dropped and a
// counter increments; serving traffic always wins over recording it.
package ingest

import (
	"fluxen/pkg/types"
)

// DefaultQueueSize is the bounded channel capacity. At Phase 1's expected
// throughput this comfortably absorbs a burst while the batch writer
// catches up; if it's ever undersized in practice, fluxen_usage_dropped_total
// (Part B.2) is exactly the signal that will show it.
const DefaultQueueSize = 10_000

// Queue is a bounded, non-blocking buffer of UsageRecords awaiting batch
// insert.
type Queue struct {
	ch      chan types.UsageRecord
	dropped Counter
}

// Counter is the minimal interface Queue needs to report drops — matched
// by *observability.Metrics.UsageDropped (a prometheus.Counter) without
// internal/ingest importing the observability or prometheus packages
// directly.
type Counter interface {
	Inc()
}

// noopCounter is used when the caller doesn't care to track drops (tests,
// or a caller that reads Queue.Dropped() itself).
type noopCounter struct{}

func (noopCounter) Inc() {}

// NewQueue creates a bounded queue with the given capacity. dropped may be
// nil, in which case drops are silently counted internally only (see
// DroppedCount).
func NewQueue(capacity int, dropped Counter) *Queue {
	if dropped == nil {
		dropped = noopCounter{}
	}
	return &Queue{ch: make(chan types.UsageRecord, capacity), dropped: dropped}
}

// Enqueue never blocks. It returns false (and increments the dropped
// counter) if the queue is full — the caller (the gateway's Account/Emit
// stage) must not treat a false return as an error worth failing the
// request over.
func (q *Queue) Enqueue(rec types.UsageRecord) bool {
	select {
	case q.ch <- rec:
		return true
	default:
		q.dropped.Inc()
		return false
	}
}

// C exposes the receive-only channel for the batch writer.
func (q *Queue) C() <-chan types.UsageRecord {
	return q.ch
}

// Close stops accepting new records. Callers must stop calling Enqueue
// before calling Close — Close does not itself synchronize against
// concurrent Enqueue calls (the gateway's shutdown sequence drains
// in-flight requests before closing the queue, per Part B's graceful
// shutdown).
func (q *Queue) Close() {
	close(q.ch)
}
