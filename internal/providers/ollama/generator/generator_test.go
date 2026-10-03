package generator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	conversationgeneration "nadir/internal/core/conversation/generation"
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

func TestGenerateSendsKeepAliveForStream(t *testing.T) {
	keepAlive := make(chan string, 2)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request ollamaChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		keepAlive <- request.KeepAlive

		w.Header().Set("Content-Type", "application/x-ndjson")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("test server does not support flushing")
			return
		}
		_, _ = fmt.Fprintln(w, `{"message":{"content":"hello"}}`)
		flusher.Flush()
		_, _ = fmt.Fprintln(w, `{"done":true}`)
		flusher.Flush()
	}))
	defer srv.Close()

	d := NewDependencies(DependenciesConfig{
		Addr:      srv.URL,
		Model:     "answer",
		KeepAlive: "5m0s",
	})
	first, err := d.Generate(context.Background(), "first")
	if err != nil {
		t.Fatalf("first Generate() error = %v", err)
	}
	for range first {
	}
	if got := <-keepAlive; got != "5m0s" {
		t.Fatalf("keep_alive = %q, want 5m0s", got)
	}

	second, err := d.Generate(context.Background(), "second")
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	for range second {
	}
	if got := <-keepAlive; got != "5m0s" {
		t.Fatalf("second keep_alive = %q, want 5m0s", got)
	}
}

func TestGenerateSendsStructuredFormatAndBoundedOptions(t *testing.T) {
	requestReceived := make(chan ollamaChatRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request ollamaChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		requestReceived <- request
		_, _ = fmt.Fprintln(w, `{"message":{"content":"ok"}}`)
		_, _ = fmt.Fprintln(w, `{"done":true}`)
	}))
	defer srv.Close()

	format := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"score": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
		},
		"required": []string{"score"},
	}
	d := NewDependencies(DependenciesConfig{
		Addr:           srv.URL,
		Model:          "judge",
		RequestTimeout: time.Second,
		Format:         format,
		Options:        map[string]any{"temperature": 0, "num_predict": 128},
	})
	events, err := d.Generate(context.Background(), "judge prompt")
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}

	request := <-requestReceived
	encodedFormat, ok := request.Format.(map[string]any)
	if !ok || encodedFormat["type"] != "object" {
		t.Fatalf("format = %#v, want JSON schema object", request.Format)
	}
	if request.Options["temperature"] != float64(0) || request.Options["num_predict"] != float64(128) {
		t.Fatalf("options = %#v, want deterministic bounded options", request.Options)
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

func TestGenerateDecodesLargeJSONEvent(t *testing.T) {
	token := strings.Repeat("x", 70*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(ollamaChatChunk{Message: ollamaMessage{Content: token}})
		_ = json.NewEncoder(w).Encode(ollamaChatChunk{Done: true})
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
		if event.Kind == conversationgeneration.EventToken {
			got.WriteString(event.Text)
		}
		if event.Kind == conversationgeneration.EventDone {
			done = true
		}
		if event.Kind == conversationgeneration.EventError {
			t.Fatalf("stream error: %v", event.Err)
		}
	}
	if got.String() != token || !done {
		t.Fatalf("large stream length=%d done=%v", got.Len(), done)
	}
}

func TestThinkingStreamKeepsReasoningSeparateAndRetainsTerminalContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(w, `{"message":{"thinking":"Internal model reasoning"}}`)
		_, _ = fmt.Fprintln(w, `{"message":{"content":"Answer."},"done":true}`)
	}))
	defer srv.Close()
	gen := NewDependencies(DependenciesConfig{Addr: srv.URL, Model: "reasoning"})
	events, err := gen.Generate(context.Background(), "Question")
	if err != nil {
		t.Fatal(err)
	}
	var answer strings.Builder
	done := false
	for ev := range events {
		switch ev.Kind {
		case conversationgeneration.EventToken:
			answer.WriteString(ev.Text)
		case conversationgeneration.EventDone:
			done = true
		case conversationgeneration.EventError:
			t.Fatalf("valid reasoning stream rejected: %v", ev.Err)
		}
	}
	if answer.String() != "Answer." || !done {
		t.Fatalf("answer=%q done=%v", answer.String(), done)
	}
}

func TestThinkingBudgetExhaustionCannotBecomeSuccessfulEmptyAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(w, `{"message":{"thinking":"Still considering"}}`)
		_, _ = fmt.Fprintln(w, `{"done":true,"done_reason":"length"}`)
	}))
	defer srv.Close()
	gen := NewDependencies(DependenciesConfig{Addr: srv.URL, Model: "reasoning"})
	events, err := gen.Generate(context.Background(), "Question")
	if err != nil {
		t.Fatal(err)
	}
	failed := false
	for ev := range events {
		if ev.Kind == conversationgeneration.EventDone {
			t.Fatal("empty answer reported as success")
		}
		if ev.Kind == conversationgeneration.EventError {
			failed = ev.Err != nil && strings.Contains(ev.Err.Error(), "without answer content")
		}
	}
	if !failed {
		t.Fatal("expected explicit empty-answer error")
	}
}
