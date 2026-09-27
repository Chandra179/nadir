// Package gates owns process-local operation gates and background work.
package gates

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Runner caps concurrent jobs and waits for accepted work during shutdown.
// Jobs inherit request values but get their own bounded lifetime.
type Runner struct {
	mu      sync.Mutex
	closed  bool
	slots   chan struct{}
	timeout time.Duration
	stop    context.Context
	cancel  context.CancelFunc
	jobs    sync.WaitGroup
}

func NewBackgroundRunner(maxConcurrent int, timeout time.Duration) *Runner {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	stop, cancel := context.WithCancel(context.Background())
	return &Runner{slots: make(chan struct{}, maxConcurrent), timeout: timeout, stop: stop, cancel: cancel}
}

// Submit returns false when the runner is full or closing.
func (r *Runner) Submit(parent context.Context, job func(context.Context)) bool {
	if r == nil || job == nil {
		return false
	}
	select {
	case r.slots <- struct{}{}:
	default:
		return false
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		<-r.slots
		return false
	}
	r.jobs.Add(1)
	r.mu.Unlock()
	go func() {
		defer r.jobs.Done()
		defer func() { <-r.slots }()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), r.timeout)
		stop := context.AfterFunc(r.stop, cancel)
		defer stop()
		defer cancel()
		job(ctx)
	}()
	return true
}

// Close rejects new jobs, waits for accepted jobs, and cancels them if the
// caller's shutdown deadline expires.
func (r *Runner) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	done := make(chan struct{})
	go func() { r.jobs.Wait(); close(done) }()
	select {
	case <-done:
		r.cancel()
		return nil
	case <-ctx.Done():
		r.cancel()
		return errors.Join(errors.New("background jobs exceeded shutdown deadline"), ctx.Err())
	}
}
