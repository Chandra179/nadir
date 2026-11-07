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

// generatorReadiness probes the configured answer provider. Workers AI is a
// hosted API: probing it would spend quota and could fail on a transient
// network error, so readiness reports only that the account and model are
// configured; an actual generated turn establishes reachability.
func generatorReadiness(cfg *config.Config) readiness.Probe {
	if cfg.Generator.Provider != "cloudflare" {
		return modelReadiness(cfg.Generator.OllamaAddr, cfg.Generator.Model, cfg.HTTP.ReadinessTimeout)
	}
	return func(context.Context) (readiness.Check, error) {
		return readiness.Check{
			Model:   cfg.Generator.Model,
			Backend: "workers-ai",
			Details: "credentials and model configured; reachability is checked by actual turns",
		}, nil
	}
}
