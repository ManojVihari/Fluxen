// Package worker is Fluxen's background job runner: a simple in-process
// ticker-based scheduler, not a distributed queue (Part C.7 of the
// implementation specification — "If this outgrows a single ticker loop,
// the escalation path is documented in Part J; it is explicitly not built
// in V1"). Each registered job runs in its own goroutine on its own
// ticker, starting with an immediate run at boot so a fresh deployment
// doesn't wait out a full interval before its first rollup/detector/etc.
// pass.
package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Job is one scheduled unit of work.
type Job struct {
	// Name identifies the job in logs (Part C.7's job table names:
	// "rollup.hourly", "rollup.daily", ...).
	Name string
	// Interval is how often Run fires after its initial immediate run.
	Interval time.Duration
	// Run performs one execution. A returned error is logged, not fatal —
	// one failed run must never stop the scheduler or crash the process.
	Run func(ctx context.Context) error
}

// Scheduler runs a fixed set of registered Jobs for the lifetime of a
// context.
type Scheduler struct {
	logger *slog.Logger
	jobs   []Job
}

func NewScheduler(logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{logger: logger}
}

// Register adds a job. Call before Run — jobs added after Run starts are
// not picked up (Phase 2 has no need for dynamic registration; every job
// is known at boot).
func (s *Scheduler) Register(j Job) {
	s.jobs = append(s.jobs, j)
}

// Run blocks until ctx is canceled, running every registered job on its
// own goroutine and ticker. It returns once every job goroutine has
// observed cancellation and exited, so a caller can rely on Run returning
// as the signal that no job is still executing.
func (s *Scheduler) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, j := range s.jobs {
		wg.Add(1)
		go func(j Job) {
			defer wg.Done()
			s.runLoop(ctx, j)
		}(j)
	}
	wg.Wait()
}

func (s *Scheduler) runLoop(ctx context.Context, j Job) {
	s.execute(ctx, j)

	ticker := time.NewTicker(j.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.execute(ctx, j)
		}
	}
}

func (s *Scheduler) execute(ctx context.Context, j Job) {
	start := time.Now()
	if err := j.Run(ctx); err != nil {
		s.logger.Error("worker: job failed", "job", j.Name, "error", err, "duration_ms", time.Since(start).Milliseconds())
		return
	}
	s.logger.Debug("worker: job completed", "job", j.Name, "duration_ms", time.Since(start).Milliseconds())
}
