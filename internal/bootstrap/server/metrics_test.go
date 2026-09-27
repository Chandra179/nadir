package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"nadir/internal/core/observability"
)

func TestMetricsHandlerReturnsSnapshot(t *testing.T) {
	recorder := observability.NewRecorder()
	recorder.Record("indexing", "success", 2*time.Millisecond)
	recorder.SetGauge("gates.indexing.active", 1)
	request := httptest.NewRequest("GET", "/debug/metrics", nil)
	response := httptest.NewRecorder()
	metricsHandler(recorder).ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("metrics status = %d, want 200", response.Code)
	}
	var snapshot observability.Snapshot
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Operations) != 1 || snapshot.Gauges["gates.indexing.active"] != 1 {
		t.Fatalf("metrics snapshot = %+v", snapshot)
	}
}
