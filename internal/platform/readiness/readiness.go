// Package readiness aggregates operational dependency checks for the HTTP
// process. It owns readiness policy, while concrete probes remain wired by the
// composition root.
package readiness

import "context"

// Check is the operational result for one dependency. The fields are also
// serialized by the HTTP transport's readiness response.
type Check struct {
	Ready       bool   `json:"ready"`
	Model       string `json:"model,omitempty"`
	LoadedModel string `json:"loaded_model,omitempty"`
	Backend     string `json:"backend,omitempty"`
	Device      string `json:"device,omitempty"`
	Error       string `json:"error,omitempty"`
	Details     string `json:"details,omitempty"`
}

// Report is ready only when every required dependency check succeeds.
type Report struct {
	Ready  bool             `json:"ready"`
	Checks map[string]Check `json:"checks"`
}

// Probe is an adapter-specific readiness check normalized at the composition
// seam. The readiness module owns aggregation and status policy, not probing.
type Probe func(context.Context) (Check, error)

// HealthProbe adapts a dependency health function to the generic readiness
// contract. Provider-specific clients remain outside this package.
func HealthProbe(check func(context.Context) error) Probe {
	if check == nil {
		return nil
	}
	return func(ctx context.Context) (Check, error) {
		return Check{}, check(ctx)
	}
}

// Dependency describes one required or disabled runtime dependency.
type Dependency struct {
	Name     string
	Probe    Probe
	Disabled bool
}

// Checker applies the common readiness policy to a fixed set of dependencies.
type Checker struct {
	dependencies []Dependency
}

// DependenciesConfig groups the probes supplied by the composition root.
type DependenciesConfig struct {
	Dependencies []Dependency
}

// NewDependencies constructs a readiness checker. The composition root
// supplies concrete adapter probes; callers receive one stable report.
func NewDependencies(cfg DependenciesConfig) *Checker {
	return &Checker{dependencies: cfg.Dependencies}
}

// Check runs all configured probes and returns a report even when one or more
// dependencies fail. This keeps diagnostics for every dependency visible to
// operators instead of stopping at the first error.
func (c *Checker) Check(ctx context.Context) Report {
	checks := make(map[string]Check, len(c.dependencies))
	ready := true

	for _, dependency := range c.dependencies {
		result := Check{}
		if dependency.Disabled {
			result.Ready = true
			result.Details = "disabled"
		} else if dependency.Probe == nil {
			result.Ready = false
			result.Error = "readiness probe is not configured"
		} else {
			var err error
			result, err = dependency.Probe(ctx)
			result.Ready = err == nil
			if err != nil && result.Error == "" {
				result.Error = err.Error()
			}
		}
		checks[dependency.Name] = result
		if !result.Ready {
			ready = false
		}
	}

	return Report{Ready: ready, Checks: checks}
}
