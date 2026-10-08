package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nadir/internal/core/observability"
)

func TestEmbedBatchHTTPContract(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantErr    string
		wantInputs int
	}{
		{name: "status error", status: http.StatusBadGateway, body: `unavailable`, wantErr: "status 502", wantInputs: 2},
		{name: "malformed json", status: http.StatusOK, body: `{`, wantErr: "decode", wantInputs: 2},
		{name: "response shape mismatch", status: http.StatusOK, body: `{"embeddings":[[1]]}`, wantErr: "got 1 embeddings for 2 inputs", wantInputs: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Input []string `json:"input"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				if len(request.Input) != tt.wantInputs && tt.wantInputs != 0 {
					t.Errorf("input count = %d, want %d", len(request.Input), tt.wantInputs)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			d := NewDependencies(DependenciesConfig{Addr: srv.URL, Model: "embed", RequestTimeout: time.Second})
			_, err := d.EmbedBatch(context.Background(), []string{"one", "two"})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("EmbedBatch() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestEmbedBatchSendsKeepAlive(t *testing.T) {
	received := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			KeepAlive string `json:"keep_alive"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		received <- request.KeepAlive
		_, _ = w.Write([]byte(`{"embeddings":[[1,2,3]]}`))
	}))
	defer srv.Close()

	d := NewDependencies(DependenciesConfig{
		Addr:      srv.URL,
		Model:     "embed",
		KeepAlive: "5m0s",
	})
	if _, err := d.EmbedBatch(context.Background(), []string{"one"}); err != nil {
		t.Fatalf("EmbedBatch() error = %v", err)
	}
	select {
	case got := <-received:
		if got != "5m0s" {
			t.Fatalf("keep_alive = %q, want 5m0s", got)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not receive embedding request")
	}
}

func TestEmbedBatchGPUPlacement(t *testing.T) {
	cpu, automatic, layers := 0, -1, 8
	for _, tt := range []struct {
		name   string
		numGPU *int
	}{
		{"unspecified", nil}, {"CPU", &cpu}, {"automatic", &automatic}, {"partial offload", &layers},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Model   string         `json:"model"`
					Options map[string]int `json:"options"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode request: %v", err)
				}
				if request.Model != "embed" {
					t.Errorf("model = %q, want embed", request.Model)
				}
				got, present := request.Options["num_gpu"]
				if present != (tt.numGPU != nil) || tt.numGPU != nil && got != *tt.numGPU {
					t.Errorf("num_gpu = %d (present=%v), requested %v", got, present, tt.numGPU)
				}
				_, _ = w.Write([]byte(`{"embeddings":[[1,2,3]]}`))
			}))
			defer srv.Close()
			d := NewDependencies(DependenciesConfig{Addr: srv.URL, Model: "embed", NumGPU: tt.numGPU})
			if _, err := d.EmbedBatch(context.Background(), []string{"one"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEmbedBatchHonorsTimeoutAndCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer srv.Close()

	d := NewDependencies(DependenciesConfig{Addr: srv.URL, Model: "embed", RequestTimeout: 10 * time.Millisecond})
	if _, err := d.EmbedBatch(context.Background(), []string{"one"}); err == nil {
		t.Fatal("EmbedBatch() succeeded after client timeout")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.EmbedBatch(ctx, []string{"one"}); err == nil {
		t.Fatal("EmbedBatch() succeeded with canceled context")
	}
}

func TestProbeVerifiesLoadedModelAndDimensions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/embed":
			_, _ = w.Write([]byte(`{"embeddings":[[1,2,3]]}`))
		case "/api/ps":
			_, _ = w.Write([]byte(`{"models":[{"name":"embed:latest"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d := NewDependencies(DependenciesConfig{
		Addr:           srv.URL,
		Model:          "embed",
		Dimensions:     3,
		RequestTimeout: time.Second,
	})
	got, err := d.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if got.ConfiguredModel != "embed" || got.LoadedModel != "embed:latest" || got.Dimensions != 3 {
		t.Fatalf("Probe() = %+v, want configured/loaded model and dimensions", got)
	}
}

func TestProbeRejectsUnexpectedDimensions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embed" {
			_, _ = w.Write([]byte(`{"embeddings":[[1,2]]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	d := NewDependencies(DependenciesConfig{Addr: srv.URL, Model: "embed", Dimensions: 3, RequestTimeout: time.Second})
	if _, err := d.Probe(context.Background()); err == nil || !strings.Contains(err.Error(), "want 3") {
		t.Fatalf("Probe() error = %v, want dimension mismatch", err)
	}
}

func TestEmbedBatchRecordsOllamaModelLoadTimings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"embeddings":[[1,2]],"total_duration":2500000000,"load_duration":2000000000}`))
	}))
	defer srv.Close()

	recorder := observability.NewRecorder()
	d := NewDependencies(DependenciesConfig{Addr: srv.URL, Model: "embed", RequestTimeout: time.Second, Telemetry: recorder})
	if _, err := d.EmbedBatch(context.Background(), []string{"one"}); err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, metric := range recorder.Snapshot().Operations {
		got[metric.Operation] = metric.DurationSumMS
	}
	if got["ollama.embed.total"] != 2500 || got["ollama.embed.load"] != 2000 || len(got) != 2 {
		t.Fatalf("recorded timings %v, want embed total 2500ms and load 2000ms", got)
	}
}

func TestEmbedBatchWithoutTimingsRecordsNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"embeddings":[[1,2]]}`))
	}))
	defer srv.Close()

	recorder := observability.NewRecorder()
	d := NewDependencies(DependenciesConfig{Addr: srv.URL, Model: "embed", RequestTimeout: time.Second, Telemetry: recorder})
	if _, err := d.EmbedBatch(context.Background(), []string{"one"}); err != nil {
		t.Fatal(err)
	}
	if operations := recorder.Snapshot().Operations; len(operations) != 0 {
		t.Fatalf("recorded %+v without reported timings", operations)
	}
}
