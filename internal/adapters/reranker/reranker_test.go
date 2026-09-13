package reranker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nadir/internal/retrieval/search"
)

func TestRerankHTTPContract(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{name: "status error", status: http.StatusBadGateway, body: "unavailable", wantErr: "status 502"},
		{name: "malformed json", status: http.StatusOK, body: `{`, wantErr: "decode"},
		{name: "response shape mismatch", status: http.StatusOK, body: `{"scores":[0.5]}`, wantErr: "score count mismatch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			d := NewDependencies(DependenciesConfig{Addr: srv.URL, RequestTimeout: time.Second})
			_, err := d.Rerank(context.Background(), "query", []search.SearchCandidate{{Text: "one"}, {Text: "two"}})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Rerank() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestRerankHonorsTimeoutAndCancellation(t *testing.T) {
	timeoutServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	d := NewDependencies(DependenciesConfig{Addr: timeoutServer.URL, RequestTimeout: 10 * time.Millisecond})
	if _, err := d.Rerank(context.Background(), "query", []search.SearchCandidate{{Text: "one"}}); err == nil {
		t.Fatal("Rerank() succeeded after client timeout")
	}
	timeoutServer.Close()

	started := make(chan struct{})
	cancelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer cancelServer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d = NewDependencies(DependenciesConfig{Addr: cancelServer.URL, RequestTimeout: time.Second})
	result := make(chan error, 1)
	go func() {
		_, err := d.Rerank(ctx, "query", []search.SearchCandidate{{Text: "one"}})
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("reranker request did not start")
	}
	cancel()
	if err := <-result; err == nil {
		t.Fatal("Rerank() succeeded with canceled request")
	}
}

func TestProbeReportsLoadedModelAndRuntime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok","model":"BAAI/bge-reranker-base","loaded_model":"BAAI/bge-reranker-base","backend":"torch-int8","device":"cpu"}`))
	}))
	defer srv.Close()

	d := NewDependencies(DependenciesConfig{Addr: srv.URL, Model: "BAAI/bge-reranker-base", RequestTimeout: time.Second})
	got, err := d.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if got.LoadedModel != "BAAI/bge-reranker-base" || got.Backend != "torch-int8" || got.Device != "cpu" {
		t.Fatalf("Probe() = %+v, want loaded model and runtime", got)
	}
}

func TestProbeReportsRunnerFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"not_ready","model":"model","error":"model load failed"}`))
	}))
	defer srv.Close()

	d := NewDependencies(DependenciesConfig{Addr: srv.URL, Model: "model", RequestTimeout: time.Second})
	if _, err := d.Probe(context.Background()); err == nil || !strings.Contains(err.Error(), "model load failed") {
		t.Fatalf("Probe() error = %v, want runner failure", err)
	}
}
