package server

import (
	"context"
	"time"

	"nadir/internal/bootstrap/readiness"
	ollamamodels "nadir/internal/providers/ollama/models"
)

// Inspect installed metadata without loading another model into a small GPU.
// An actual generated turn is still needed to establish inference capacity.
func modelReadiness(addr, model string, timeout time.Duration) readiness.Probe {
	checker := ollamamodels.NewDependencies(ollamamodels.DependenciesConfig{
		Addr: addr, Model: model, RequestTimeout: timeout,
	})
	return func(ctx context.Context) (readiness.Check, error) {
		return readiness.Check{Model: model, Details: "installed model metadata; inference capacity is checked by actual turns"}, checker.Check(ctx)
	}
}
