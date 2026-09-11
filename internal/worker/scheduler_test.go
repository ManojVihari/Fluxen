package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler_RunsImmediatelyAtBoot(t *testing.T) {
	var calls int32
	sched := NewScheduler(nil)
	sched.Register(Job{
		Name:     "test.immediate",
		Interval: time.Hour, // long enough that only the immediate run fires during the test
		Run: func(ctx context.Context) error {
			atomic.AddInt32(&calls, 1)
			return nil
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	sched.Run(ctx)

	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("expected exactly 1 immediate call, got %d", calls)
	}
}

func TestScheduler_RunsOnEveryTick(t *testing.T) {
	var calls int32
	sched := NewScheduler(nil)
	sched.Register(Job{
		Name:     "test.tick",
		Interval: 15 * time.Millisecond,
		Run: func(ctx context.Context) error {
			atomic.AddInt32(&calls, 1)
			return nil
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Millisecond)
	defer cancel()
	sched.Run(ctx)

	// Immediate run + roughly 4 ticks in ~70ms at a 15ms interval —
	// assert "more than just the immediate run" rather than an exact
	// count, since ticker timing isn't guaranteed to the millisecond.
	if calls < 3 {
		t.Errorf("expected multiple ticks to have fired, got %d calls", calls)
	}
}

func TestScheduler_FailedJobDoesNotStopScheduler(t *testing.T) {
	var calls int32
	sched := NewScheduler(nil)
	sched.Register(Job{
		Name:     "test.failing",
		Interval: 10 * time.Millisecond,
		Run: func(ctx context.Context) error {
			atomic.AddInt32(&calls, 1)
			return errors.New("boom")
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	sched.Run(ctx)

	if calls < 2 {
		t.Errorf("expected the scheduler to keep ticking after a job error, got %d calls", calls)
	}
}

func TestScheduler_RunReturnsAfterContextCanceled(t *testing.T) {
	sched := NewScheduler(nil)
	sched.Register(Job{
		Name:     "test.noop",
		Interval: time.Hour,
		Run:      func(ctx context.Context) error { return nil },
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sched.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected Run to return promptly after context cancellation")
	}
}

func TestScheduler_MultipleJobsRunIndependently(t *testing.T) {
	var callsA, callsB int32
	sched := NewScheduler(nil)
	sched.Register(Job{Name: "a", Interval: time.Hour, Run: func(ctx context.Context) error {
		atomic.AddInt32(&callsA, 1)
		return nil
	}})
	sched.Register(Job{Name: "b", Interval: time.Hour, Run: func(ctx context.Context) error {
		atomic.AddInt32(&callsB, 1)
		return nil
	}})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	sched.Run(ctx)

	if callsA != 1 || callsB != 1 {
		t.Errorf("expected both jobs to run once independently, got a=%d b=%d", callsA, callsB)
	}
}
