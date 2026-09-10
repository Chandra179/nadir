package reranker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nadir/internal/store"
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
			_, err := d.Rerank(context.Background(), "query", []store.ScoredChunk{{Text: "one"}, {Text: "two"}})
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
	if _, err := d.Rerank(context.Background(), "query", []store.ScoredChunk{{Text: "one"}}); err == nil {
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
		_, err := d.Rerank(ctx, "query", []store.ScoredChunk{{Text: "one"}})
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
