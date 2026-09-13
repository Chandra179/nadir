// Package inference provides process-local admission control for expensive
// model work. It is intentionally a concrete Module: the standard-library
// channel implementation is the only local Adapter needed today.
package inference

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	defaultMaxConcurrent = 1
	defaultQueueTimeout  = 30 * time.Second
)

// ErrCapacity reports that an inference request could not acquire a local
// execution slot before the configured queue timeout.
var ErrCapacity = errors.New("inference capacity exhausted")

// Gate bounds concurrent work and prevents an unbounded wait from turning
// model contention into an unbounded HTTP request. A slot is held until the
// returned release function is called, which is important for streaming
// generation where the model remains active after HTTP headers arrive.
type Gate struct {
	slots        chan struct{}
	queueTimeout time.Duration
}

// NewGate constructs a process-local inference gate. Non-positive values use
// conservative defaults so direct package tests cannot accidentally create an
// unbounded or permanently blocked Adapter.
func NewGate(maxConcurrent int, queueTimeout time.Duration) *Gate {
	if maxConcurrent <= 0 {
		maxConcurrent = defaultMaxConcurrent
	}
	if queueTimeout <= 0 {
		queueTimeout = defaultQueueTimeout
	}
	return &Gate{
		slots:        make(chan struct{}, maxConcurrent),
		queueTimeout: queueTimeout,
	}
}

// Acquire waits for one execution slot. The caller must invoke the returned
// release function exactly once after the complete model operation finishes.
func (g *Gate) Acquire(ctx context.Context) (func(), error) {
	if g == nil {
		return nil, fmt.Errorf("%w: gate is nil", ErrCapacity)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	timer := time.NewTimer(g.queueTimeout)
	defer timer.Stop()
	select {
	case g.slots <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() { <-g.slots })
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, fmt.Errorf("%w after %s", ErrCapacity, g.queueTimeout)
	}
}
