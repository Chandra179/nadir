// Package gates owns process-wide backpressure for expensive and
// destructive operations. It is intentionally local to one executable;
// distributed deployments need a shared coordinator at this seam.
package gates

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"nadir/internal/core/observability"
)

// Operation identifies one independently tunable runtime capacity budget.
// LLM/embedding concurrency is delegated to the Ollama scheduler, so only
// single-writer indexing and destructive mutations are gated here.
type Operation string

const (
	Indexing    Operation = "indexing"
	Destructive Operation = "destructive"
)

// OperationConfig bounds concurrent work and the time it may wait for a
// process-local slot.
type OperationConfig struct {
	MaxConcurrent int
	QueueTimeout  time.Duration
}

// Config defines every process-wide gate budget.
type Config struct {
	Indexing    OperationConfig
	Destructive OperationConfig
	Recorder    *observability.Recorder
}

type operationStats struct {
	active        atomic.Int64
	waiting       atomic.Int64
	peakActive    atomic.Int64
	maxConcurrent int
	recorder      *observability.Recorder
	operation     Operation
}

// Controller owns one Gate for each operation. The gates are shared by every
// Adapter and domain Module composed into the executable, so concurrent HTTP
// requests cannot multiply a per-request limit without meeting the same
// process-wide budget.
type Controller struct {
	gates map[Operation]*Gate
	stats map[Operation]*operationStats
}

// New constructs a process-wide gate controller. Config is expected to
// be validated before composition; Gate still applies defensive
// defaults for direct package tests.
func New(cfg Config) *Controller {
	configs := map[Operation]OperationConfig{
		Indexing: cfg.Indexing, Destructive: cfg.Destructive,
	}
	c := &Controller{gates: make(map[Operation]*Gate), stats: make(map[Operation]*operationStats)}
	for operation, operationConfig := range configs {
		maxConcurrent := operationConfig.MaxConcurrent
		if maxConcurrent <= 0 {
			maxConcurrent = 1
		}
		stats := &operationStats{
			maxConcurrent: maxConcurrent,
			recorder:      cfg.Recorder,
			operation:     operation,
		}
		c.gates[operation] = NewGate(operationConfig.MaxConcurrent, operationConfig.QueueTimeout)
		c.stats[operation] = stats
		stats.setGauges()
	}
	return c
}

// Gate returns the shared gate for an operation. Unknown operations are
// rejected with a defensive gate rather than returning nil to a caller.
func (c *Controller) Gate(operation Operation) *Gate {
	if c != nil {
		if gate, ok := c.gates[operation]; ok {
			return gate
		}
	}
	return NewGate(1, time.Second)
}

// Acquire is a convenience for callers that need one operation without
// retaining the concrete Gate.
func (c *Controller) Acquire(ctx context.Context, operation Operation) (func(), error) {
	if c == nil {
		return nil, fmt.Errorf("%w: gate controller is nil", ErrCapacity)
	}
	gate := c.Gate(operation)
	stats := c.stats[operation]
	started := time.Now()
	if stats != nil {
		stats.waiting.Add(1)
		stats.setGauges()
	}
	release, err := gate.Acquire(ctx)
	if stats != nil {
		stats.waiting.Add(-1)
	}
	if err != nil {
		if stats != nil {
			stats.record("rejected", time.Since(started))
		}
		return nil, err
	}
	if stats == nil {
		return release, nil
	}
	active := stats.active.Add(1)
	for {
		old := stats.peakActive.Load()
		if active <= old || stats.peakActive.CompareAndSwap(old, active) {
			break
		}
	}
	stats.record("acquired", time.Since(started))
	stats.setGauges()
	var once sync.Once
	return func() {
		once.Do(func() {
			release()
			stats.active.Add(-1)
			stats.setGauges()
		})
	}, nil
}

// AcquireFunc returns a narrow gate seam for dependency injection.
func (c *Controller) AcquireFunc(operation Operation) func(context.Context) (func(), error) {
	return func(ctx context.Context) (func(), error) { return c.Acquire(ctx, operation) }
}

func (s *operationStats) record(outcome string, duration time.Duration) {
	if s.recorder != nil {
		s.recorder.Record("gates."+string(s.operation), outcome, duration)
	}
}

func (s *operationStats) setGauges() {
	if s.recorder == nil {
		return
	}
	prefix := "gates." + string(s.operation)
	s.recorder.SetGauge(prefix+".active", float64(s.active.Load()))
	s.recorder.SetGauge(prefix+".waiting", float64(s.waiting.Load()))
	s.recorder.SetGauge(prefix+".peak_active", float64(s.peakActive.Load()))
	s.recorder.SetGauge(prefix+".max_concurrent", float64(s.maxConcurrent))
}
