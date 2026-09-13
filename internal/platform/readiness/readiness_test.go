package readiness

import (
	"context"
	"errors"
	"testing"
)

func TestCheckerReportsAllDependencies(t *testing.T) {
	checker := NewDependencies(DependenciesConfig{Dependencies: []Dependency{
		Dependency{
			Name: "qdrant",
			Probe: func(context.Context) (Check, error) {
				return Check{Details: "reachable"}, nil
			},
		},
		Dependency{Name: "reranker", Disabled: true},
		Dependency{
			Name: "embedding",
			Probe: func(context.Context) (Check, error) {
				return Check{Model: "embed", Error: "model unavailable"}, errors.New("model unavailable")
			},
		},
	}})

	report := checker.Check(context.Background())
	if report.Ready {
		t.Fatal("report is ready despite a failed dependency")
	}
	if !report.Checks["qdrant"].Ready {
		t.Fatal("qdrant should be ready")
	}
	if !report.Checks["reranker"].Ready || report.Checks["reranker"].Details != "disabled" {
		t.Fatalf("disabled reranker check = %+v", report.Checks["reranker"])
	}
	if report.Checks["embedding"].Ready || report.Checks["embedding"].Error == "" {
		t.Fatalf("embedding check = %+v", report.Checks["embedding"])
	}
}

func TestCheckerReportsReadyWhenNoDependenciesFail(t *testing.T) {
	report := NewDependencies(DependenciesConfig{Dependencies: []Dependency{{Name: "qdrant", Disabled: true}}}).Check(context.Background())
	if !report.Ready {
		t.Fatalf("report = %+v, want ready", report)
	}
}

func TestCheckerTreatsMissingRequiredProbeAsNotReady(t *testing.T) {
	report := NewDependencies(DependenciesConfig{Dependencies: []Dependency{{Name: "qdrant"}}}).Check(context.Background())
	if report.Ready {
		t.Fatalf("report = %+v, want not ready", report)
	}
	check := report.Checks["qdrant"]
	if check.Ready || check.Error != "readiness probe is not configured" {
		t.Fatalf("qdrant check = %+v, want missing-probe error", check)
	}
}
