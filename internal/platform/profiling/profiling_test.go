package profiling

import (
	"context"
	"net/http/httptest"
	"testing"

	config "nadir/internal/platform/configuration"
)

func TestStartDisabledDoesNotOpenAListener(t *testing.T) {
	stop, err := Start(context.Background(), config.ProfilingConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
}

func TestStartServesPprofAndStops(t *testing.T) {
	stop, err := Start(context.Background(), config.ProfilingConfig{Enabled: true, Addr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	secondStop, err := Start(ctx, config.ProfilingConfig{Enabled: true, Addr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := secondStop(); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/debug/pprof/", nil))
	if recorder.Code != 200 {
		t.Fatalf("pprof index status = %d, want 200", recorder.Code)
	}
}
