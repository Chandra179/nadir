package server

import (
	"context"
	"time"

	"nadir/internal/bootstrap/configuration"
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

// generatorReadiness probes the configured answer provider.
func generatorReadiness(cfg *config.Config) readiness.Probe {
	return roleReadiness(cfg.Generator.Provider, cfg.Generator.OllamaAddr, cfg.Generator.Model, cfg.HTTP.ReadinessTimeout)
}

// roleReadiness probes one LLM role. Workers AI is a hosted API: probing it
// would spend quota and could fail on a transient network error, so cloudflare
// roles report configuration only and an actual request establishes
// reachability. Ollama roles inspect installed model metadata.
func roleReadiness(provider, addr, model string, timeout time.Duration) readiness.Probe {
	if provider != "cloudflare" {
		return modelReadiness(addr, model, timeout)
	}
	return func(context.Context) (readiness.Check, error) {
		return readiness.Check{
			Model:   model,
			Backend: "workers-ai",
			Details: "credentials and model configured; reachability is checked by actual requests",
		}, nil
	}
}
