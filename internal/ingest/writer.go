package ingest

import (
	"context"
	"log/slog"
	"time"

	"fluxen/pkg/types"
)

// DefaultBatchSize and DefaultFlushInterval control how often the writer
// flushes to Postgres: whichever comes first. Phase 1 traffic volumes
// don't need these to be configurable; they can become config knobs if a
// later phase's load testing shows a reason to.
const (
	DefaultBatchSize     = 200
	DefaultFlushInterval = 500 * time.Millisecond
)

// BatchInserter is the storage dependency Writer needs — matched by
// *store.Requests — kept as an interface so this package is testable
// without a database (Rule 8).
type BatchInserter interface {
	InsertBatch(ctx context.Context, records []types.UsageRecord) error
}

// Writer drains a Queue and flushes accumulated UsageRecords to Postgres
// in batches, by size or by time, whichever comes first.
type Writer struct {
	queue         *Queue
	store         BatchInserter
	batchSize     int
	flushInterval time.Duration
	logger        *slog.Logger
}

func NewWriter(queue *Queue, store BatchInserter, logger *slog.Logger) *Writer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Writer{
		queue:         queue,
		store:         store,
		batchSize:     DefaultBatchSize,
		flushInterval: DefaultFlushInterval,
		logger:        logger,
	}
}

// Run drains the queue until ctx is canceled, then flushes whatever
// remains and returns. It is meant to run in its own goroutine for the
// lifetime of the process.
func (w *Writer) Run(ctx context.Context) {
	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()

	buf := make([]types.UsageRecord, 0, w.batchSize)

	flush := func() {
		if len(buf) == 0 {
			return
		}
		// A fresh, short-lived context: ctx may already be canceled
		// (shutdown) by the time we flush, but a batch already pulled off
		// the queue should still make it to Postgres if the database is
		// reachable — dropping it here would be strictly worse than the
		// hot-path drop-on-full behavior, since these records already
		// represent requests we accepted responsibility for.
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := w.store.InsertBatch(flushCtx, buf); err != nil {
			w.logger.Error("ingest: failed to flush usage records", "count", len(buf), "error", err)
		}
		buf = buf[:0]
	}

	for {
		select {
		case <-ctx.Done():
			// Drain whatever is already buffered in the channel before
			// exiting, since Close() may not have been called yet.
			for {
				select {
				case rec, ok := <-w.queue.C():
					if !ok {
						flush()
						return
					}
					buf = append(buf, rec)
					if len(buf) >= w.batchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}

		case rec, ok := <-w.queue.C():
			if !ok {
				flush()
				return
			}
			buf = append(buf, rec)
			if len(buf) >= w.batchSize {
				flush()
			}

		case <-ticker.C:
			flush()
		}
	}
}
