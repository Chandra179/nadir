package enrichment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestContextualIntroCleansOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]string{"content": "  \"This chunk is from a calculus cheat sheet covering derivative rules.\"  "},
		})
	}))
	defer srv.Close()

	d := NewDependencies(DependenciesConfig{ContextualAddr: srv.URL, ContextualModel: "test"})
	intro, err := d.ContextualIntro(context.Background(), "excerpt", "chunk text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if intro != "This chunk is from a calculus cheat sheet covering derivative rules." {
		t.Errorf("intro not cleaned: %q", intro)
	}
}

func TestEnrichmentHTTPContract(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{name: "status error", status: http.StatusBadGateway, body: "unavailable", wantErr: "status 502"},
		{name: "malformed json", status: http.StatusOK, body: `{`, wantErr: "decode"},
		{name: "response shape mismatch", status: http.StatusOK, body: `{}`, wantErr: "empty contextual intro"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			d := NewDependencies(DependenciesConfig{ContextualAddr: srv.URL, ContextualModel: "test", RequestTimeout: time.Second})
			_, err := d.ContextualIntro(context.Background(), "excerpt", "chunk")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ContextualIntro() error = %v, want %q", err, tt.wantErr)
			}
		})
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": ""}})
	}))
	defer srv.Close()
	d := NewDependencies(DependenciesConfig{ContextualAddr: srv.URL, ContextualModel: "test"})
	if _, err := d.ContextualIntro(context.Background(), "excerpt", "chunk"); err == nil || !strings.Contains(err.Error(), "empty contextual intro") {
		t.Fatalf("ContextualIntro() error = %v, want empty response-shape error", err)
	}
}

func TestEnrichmentHonorsTimeoutAndCancellation(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer srv.Close()

	d := NewDependencies(DependenciesConfig{ContextualAddr: srv.URL, ContextualModel: "test", RequestTimeout: 10 * time.Millisecond})
	if _, err := d.ContextualIntro(context.Background(), "excerpt", "chunk"); err == nil {
		t.Fatal("ContextualIntro() succeeded after client timeout")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := d.ContextualIntro(ctx, "excerpt", "chunk")
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("enrichment request did not start")
	}
	cancel()
	if err := <-result; err == nil {
		t.Fatal("HypotheticalQuestions() succeeded with canceled request")
	}
}
