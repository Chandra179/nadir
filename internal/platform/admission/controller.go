// Package admission provides process-wide backpressure for expensive and
// destructive operations. It is intentionally local to one executable;
// distributed deployments need a shared coordinator at this seam.
package admission

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"nadir/internal/platform/inference"
	"nadir/internal/platform/observability"
)

// Operation identifies one independently tunable runtime capacity budget.
type Operation string

const (
	Retrieval   Operation = "retrieval"
	Reranking   Operation = "reranking"
	Generation  Operation = "generation"
	Embedding   Operation = "embedding"
	Indexing    Operation = "indexing"
	Destructive Operation = "destructive"
)

// OperationConfig bounds concurrent work and the time it may wait for a
// process-local slot.
type OperationConfig struct {
	MaxConcurrent int
	QueueTimeout  time.Duration
}

// Config defines every process-wide admission budget.
type Config struct {
	Retrieval   OperationConfig
	Reranking   OperationConfig
	Generation  OperationConfig
	Embedding   OperationConfig
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
	gates map[Operation]*inference.Gate
	stats map[Operation]*operationStats
}

// New constructs a process-wide admission controller. Config is expected to
// be validated before composition; inference.Gate still applies defensive
// defaults for direct package tests.
func New(cfg Config) *Controller {
	configs := map[Operation]OperationConfig{
		Retrieval: cfg.Retrieval, Reranking: cfg.Reranking, Generation: cfg.Generation,
		Embedding: cfg.Embedding, Indexing: cfg.Indexing, Destructive: cfg.Destructive,
	}
	c := &Controller{gates: make(map[Operation]*inference.Gate), stats: make(map[Operation]*operationStats)}
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
		c.gates[operation] = inference.NewGate(operationConfig.MaxConcurrent, operationConfig.QueueTimeout)
		c.stats[operation] = stats
		stats.setGauges()
	}
	return c
}

// Gate returns the shared gate for an operation. Unknown operations are
// rejected with a defensive gate rather than returning nil to a caller.
func (c *Controller) Gate(operation Operation) *inference.Gate {
	if c != nil {
		if gate, ok := c.gates[operation]; ok {
			return gate
		}
	}
	return inference.NewGate(1, time.Second)
}

// Acquire is a convenience for callers that need one operation without
// retaining the concrete Gate.
func (c *Controller) Acquire(ctx context.Context, operation Operation) (func(), error) {
	if c == nil {
		return nil, fmt.Errorf("%w: admission controller is nil", inference.ErrCapacity)
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

// AcquireFunc returns a narrow admission seam for dependency injection.
func (c *Controller) AcquireFunc(operation Operation) func(context.Context) (func(), error) {
	return func(ctx context.Context) (func(), error) { return c.Acquire(ctx, operation) }
}

func (s *operationStats) record(outcome string, duration time.Duration) {
	if s.recorder != nil {
		s.recorder.Record("admission."+string(s.operation), outcome, duration)
	}
}

func (s *operationStats) setGauges() {
	if s.recorder == nil {
		return
	}
	prefix := "admission." + string(s.operation)
	s.recorder.SetGauge(prefix+".active", float64(s.active.Load()))
	s.recorder.SetGauge(prefix+".waiting", float64(s.waiting.Load()))
	s.recorder.SetGauge(prefix+".peak_active", float64(s.peakActive.Load()))
	s.recorder.SetGauge(prefix+".max_concurrent", float64(s.maxConcurrent))
}
