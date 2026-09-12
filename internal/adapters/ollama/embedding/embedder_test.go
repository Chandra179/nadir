package embedder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
