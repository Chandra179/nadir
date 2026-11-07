package generator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	conversationgeneration "nadir/internal/core/conversation/generation"
	"nadir/internal/core/observability"
)

func newTestGenerator(t *testing.T, srv *httptest.Server) *dependencies {
	t.Helper()
	d, err := NewDependencies(DependenciesConfig{
		AccountID: "acct", APIToken: "secret-token", Model: "@cf/test/model",
		BaseURL: srv.URL, RequestTimeout: time.Second, MaxOutputTokens: 77,
	})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func collect(events <-chan conversationgeneration.Event) (text string, done bool, errs []string) {
	var b strings.Builder
	for ev := range events {
		switch ev.Kind {
		case conversationgeneration.EventToken:
			b.WriteString(ev.Text)
		case conversationgeneration.EventDone:
			done = true
		case conversationgeneration.EventError:
			errs = append(errs, ev.Err.Error())
		}
	}
	return b.String(), done, errs
}

func sse(w http.ResponseWriter, lines ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, l := range lines {
		_, _ = w.Write([]byte("data: " + l + "\n\n"))
	}
}

func TestNewDependenciesRequiresCredentialsAndModel(t *testing.T) {
	for name, cfg := range map[string]DependenciesConfig{
		"account": {APIToken: "t", Model: "m"},
		"token":   {AccountID: "a", Model: "m"},
		"model":   {AccountID: "a", APIToken: "t"},
	} {
		if _, err := NewDependencies(cfg); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: error = %v", name, err)
		}
	}
}

func TestGenerateRequestContractAndStream(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		sse(w,
			`{"choices":[{"delta":{"role":"assistant"}}]}`,
			`{"choices":[{"delta":{"reasoning_content":"thinking"}}]}`,
			`{"choices":[{"delta":{"content":"hel"}}]}`,
			`{"choices":[{"delta":{"content":"lo"},"finish_reason":null}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":3}}`,
			`[DONE]`,
		)
	}))
	defer srv.Close()

	rec := observability.NewRecorder()
	d := newTestGenerator(t, srv)
	d.telemetry = rec
	events, err := d.Generate(context.Background(), "question")
	if err != nil {
		t.Fatal(err)
	}
	text, done, errs := collect(events)
	if text != "hello" || !done || len(errs) != 0 {
		t.Fatalf("stream = %q done=%v errs=%v", text, done, errs)
	}
	if gotPath != "/accounts/acct/ai/v1/chat/completions" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("authorization header = %q", gotAuth)
	}
	if gotBody.Model != "@cf/test/model" || !gotBody.Stream || gotBody.MaxTokens != 77 || gotBody.Temperature != 0 ||
		len(gotBody.Messages) != 1 || gotBody.Messages[0].Role != "user" || gotBody.Messages[0].Content != "question" {
		t.Errorf("request body = %+v", gotBody)
	}
}

func TestGenerateStatusErrorsDoNotLeakToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":[{"message":"model requires paid plan"}]}`))
	}))
	defer srv.Close()
	_, err := newTestGenerator(t, srv).Generate(context.Background(), "q")
	if err == nil || !strings.Contains(err.Error(), "status 403") || !strings.Contains(err.Error(), "model requires paid plan") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("error leaks token: %v", err)
	}
}

func TestGenerateStreamFailures(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{"malformed", []string{`not-json`}, "stream decode"},
		{"provider error", []string{`{"error":{"message":"overloaded"}}`}, "overloaded"},
		{"no answer", []string{`{"choices":[{"delta":{"reasoning_content":"x"}}]}`, `[DONE]`}, "without answer content"},
		{"truncated", []string{`{"choices":[{"delta":{"content":"partial"}}]}`}, "before completion"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sse(w, tt.lines...) }))
			defer srv.Close()
			events, err := newTestGenerator(t, srv).Generate(context.Background(), "q")
			if err != nil {
				t.Fatal(err)
			}
			_, done, errs := collect(events)
			if done || len(errs) != 1 || !strings.Contains(errs[0], tt.want) {
				t.Fatalf("done=%v errs=%v, want %q", done, errs, tt.want)
			}
		})
	}
}

func TestGenerateCancellationClosesStream(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sse(w, `{"choices":[{"delta":{"content":"a"}}]}`)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	events, err := newTestGenerator(t, srv).Generate(ctx, "q")
	if err != nil {
		t.Fatal(err)
	}
	<-events
	cancel()
	select {
	case _, ok := <-events:
		for ok { // drain any error event emitted while cancelling
			_, ok = <-events
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not close after cancellation")
	}
}
