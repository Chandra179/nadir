package observability

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"
)

func TestErrorLabelIsBoundedAndOperational(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want string
	}{
		{name: "canceled", err: context.Canceled, want: "canceled"},
		{name: "deadline", err: context.DeadlineExceeded, want: "deadline_exceeded"},
		{name: "generation", err: errors.New("generation failed"), want: "generation_error"},
		{name: "status", err: errors.New("adapter status 503"), want: "remote_status"},
		{name: "decode", err: errors.New("decode response"), want: "malformed_response"},
		{name: "shape", err: errors.New("score count mismatch"), want: "response_shape"},
		{name: "unknown", err: errors.New("something unexpected"), want: "unknown"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ErrorLabel(tt.err); got != tt.want {
				t.Fatalf("ErrorLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSpanRecordsBoundedMetricAndCorrelationIDs(t *testing.T) {
	recorder := NewRecorder()
	ctx := WithRequestID(context.Background(), "req-test")
	ctx, parent := Start(ctx, recorder, nil, "chat")
	if parent.ID() == "" || TraceID(ctx) == "" || RequestID(ctx) != "req-test" {
		t.Fatalf("missing parent correlation IDs: operation=%q trace=%q request=%q", parent.ID(), TraceID(ctx), RequestID(ctx))
	}
	childCtx, child := Start(ctx, recorder, nil, "retrieval")
	if OperationID(childCtx) != child.ID() {
		t.Fatalf("child operation in context = %q, want %q", OperationID(childCtx), child.ID())
	}
	child.End("success", nil)
	parent.End("error", context.DeadlineExceeded)
	parent.End("success", nil)

	snapshot := recorder.Snapshot()
	if len(snapshot.Operations) != 2 {
		t.Fatalf("metrics = %+v, want two operation outcomes", snapshot.Operations)
	}
	if snapshot.Operations[0].Operation != "chat" || snapshot.Operations[0].Outcome != "error" {
		t.Fatalf("metrics are not sorted or labeled: %+v", snapshot.Operations)
	}
}

func TestMetricsHandlerReturnsSnapshot(t *testing.T) {
	recorder := NewRecorder()
	recorder.Record("indexing", "success", 2*time.Millisecond)
	recorder.SetGauge("admission.indexing.active", 1)
	request := httptest.NewRequest("GET", "/debug/metrics", nil)
	response := httptest.NewRecorder()
	recorder.Handler().ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("metrics status = %d, want 200", response.Code)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Operations) != 1 || snapshot.Gauges["admission.indexing.active"] != 1 {
		t.Fatalf("metrics snapshot = %+v", snapshot)
	}
}
