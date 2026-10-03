package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nadir/internal/bootstrap/readiness"
)

func TestReadinessUsesEachEnabledModelEndpointWithoutInference(t *testing.T) {
	var calls []string
	endpoint := func(installed string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/show" {
				t.Errorf("readiness must not start inference: %s", r.URL.Path)
			}
			var body struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			calls = append(calls, body.Model)
			if body.Model != installed {
				w.WriteHeader(404)
				return
			}
			_, _ = w.Write([]byte(`{"details":{"family":"gemma"}}`))
		}))
	}
	generator := endpoint("answers")
	defer generator.Close()
	rewriter := endpoint("another-model")
	defer rewriter.Close()
	deps := []readiness.Dependency{
		{Name: "generator", Probe: modelReadiness(generator.URL, "answers", time.Second)},
		{Name: "rewriter", Probe: modelReadiness(rewriter.URL, "followups", time.Second)},
	}
	report := readiness.NewDependencies(readiness.DependenciesConfig{Dependencies: deps}).Check(context.Background())
	if report.Ready || !report.Checks["generator"].Ready || report.Checks["generator"].Model != "answers" || !strings.Contains(report.Checks["rewriter"].Error, "ollama pull followups") {
		t.Fatalf("missing enabled role must block readiness with its own installation hint: %+v", report)
	}
	deps[1].Disabled = true
	calls = nil
	report = readiness.NewDependencies(readiness.DependenciesConfig{Dependencies: deps}).Check(context.Background())
	if !report.Ready || len(calls) != 1 || calls[0] != "answers" {
		t.Fatalf("disabled role was probed: %+v calls=%v", report, calls)
	}
}
