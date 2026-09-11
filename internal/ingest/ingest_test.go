package ingest

import (
	"context"
	"sync"
	"testing"
	"time"

	"fluxen/pkg/types"
)

func TestQueue_EnqueueDropsWhenFull(t *testing.T) {
	q := NewQueue(1, nil)

	if !q.Enqueue(types.UsageRecord{ID: "a"}) {
		t.Fatal("expected the first enqueue into a size-1 queue to succeed")
	}
	if q.Enqueue(types.UsageRecord{ID: "b"}) {
		t.Fatal("expected the second enqueue into a full size-1 queue to be dropped")
	}
}

func TestQueue_EnqueueIncrementsDroppedCounter(t *testing.T) {
	counter := &countingCounter{}
	q := NewQueue(1, counter)

	q.Enqueue(types.UsageRecord{ID: "a"})
	q.Enqueue(types.UsageRecord{ID: "b"}) // dropped
	q.Enqueue(types.UsageRecord{ID: "c"}) // dropped

	if counter.n != 2 {
		t.Errorf("expected the dropped counter to be incremented twice, got %d", counter.n)
	}
}

type countingCounter struct {
	mu sync.Mutex
	n  int
}

func (c *countingCounter) Inc() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
}

type fakeInserter struct {
	mu      sync.Mutex
	batches [][]types.UsageRecord
}

func (f *fakeInserter) InsertBatch(_ context.Context, records []types.UsageRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := append([]types.UsageRecord(nil), records...)
	f.batches = append(f.batches, cp)
	return nil
}

func (f *fakeInserter) totalRecords() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, b := range f.batches {
		n += len(b)
	}
	return n
}

func TestWriter_FlushesBySize(t *testing.T) {
	q := NewQueue(100, nil)
	store := &fakeInserter{}
	w := NewWriter(q, store, nil)
	w.batchSize = 3
	w.flushInterval = time.Hour // effectively disabled for this test

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	for i := 0; i < 3; i++ {
		q.Enqueue(types.UsageRecord{ID: "rec"})
	}

	waitFor(t, func() bool { return store.totalRecords() == 3 })

	cancel()
	<-done
}

func TestWriter_FlushesByTime(t *testing.T) {
	q := NewQueue(100, nil)
	store := &fakeInserter{}
	w := NewWriter(q, store, nil)
	w.batchSize = 1000 // large enough that only the ticker triggers a flush
	w.flushInterval = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	q.Enqueue(types.UsageRecord{ID: "rec"})

	waitFor(t, func() bool { return store.totalRecords() == 1 })

	cancel()
	<-done
}

func TestWriter_FlushesRemainderOnShutdown(t *testing.T) {
	q := NewQueue(100, nil)
	store := &fakeInserter{}
	w := NewWriter(q, store, nil)
	w.batchSize = 1000
	w.flushInterval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	q.Enqueue(types.UsageRecord{ID: "rec-1"})
	q.Enqueue(types.UsageRecord{ID: "rec-2"})

	// Give the writer a moment to have pulled these off the channel into
	// its buffer before we cancel, so this exercises "flush on shutdown"
	// rather than "drain the channel on shutdown" (both paths exist in
	// Run, both should end up flushing).
	time.Sleep(20 * time.Millisecond)

	cancel()
	<-done

	if store.totalRecords() != 2 {
		t.Errorf("expected both records to be flushed on shutdown, got %d", store.totalRecords())
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition was not met within the timeout")
}
