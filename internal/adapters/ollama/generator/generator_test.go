package generator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	conversationgeneration "nadir/internal/conversation/generation"
)

func TestGenerateHTTPContractAndStreamClosure(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{name: "status error", status: http.StatusBadGateway, body: "unavailable", wantErr: "status 502"},
		{name: "malformed stream", status: http.StatusOK, body: "not-json\n", wantErr: "stream decode"},
		{name: "response shape mismatch", status: http.StatusOK, body: "{}\n", wantErr: "missing message.content"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			d := NewDependencies(DependenciesConfig{Addr: srv.URL, Model: "answer", RequestTimeout: time.Second})
			events, err := d.Generate(context.Background(), "prompt")
			if tt.status != http.StatusOK {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Generate() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var streamErr string
			for event := range events {
				if event.Kind == conversationgeneration.EventError && event.Err != nil {
					streamErr = event.Err.Error()
				}
			}
			if !strings.Contains(streamErr, tt.wantErr) {
				t.Fatalf("stream error = %q, want %q", streamErr, tt.wantErr)
			}
		})
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":{"content":"hello"}}` + "\n" + `{"done":true}` + "\n"))
	}))
	defer srv.Close()
	d := NewDependencies(DependenciesConfig{Addr: srv.URL, Model: "answer", RequestTimeout: time.Second})
	events, err := d.Generate(context.Background(), "prompt")
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	var done bool
	for event := range events {
		switch event.Kind {
		case conversationgeneration.EventToken:
			got.WriteString(event.Text)
		case conversationgeneration.EventDone:
			done = true
		}
	}
	if got.String() != "hello" || !done {
		t.Fatalf("stream = %q, done=%v, want hello and done", got.String(), done)
	}
}

func TestGenerateHonorsCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := NewDependencies(DependenciesConfig{Addr: srv.URL, Model: "answer", RequestTimeout: time.Second})
	if _, err := d.Generate(ctx, "prompt"); err == nil {
		t.Fatal("Generate() succeeded with canceled context")
	}
}
