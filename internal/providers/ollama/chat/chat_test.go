package chat

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

func reply(content string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": content}})
	}
}

// The two shipped roles differ only in sampling; their payloads were pinned
// before the transport moved here and must not change.
func TestRequestPayloadIsStable(t *testing.T) {
	for _, tc := range []struct {
		name        string
		temperature float64
		maxTokens   int
		wantOptions map[string]any
	}{
		{"rewriter", 0, 128, map[string]any{"temperature": float64(0), "num_predict": float64(128)}},
		{"enrichment", 0.2, 0, map[string]any{"temperature": 0.2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			var path string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path = r.URL.Path
				_ = json.NewDecoder(r.Body).Decode(&got)
				reply("ok")(w, r)
			}))
			defer srv.Close()
			off := false
			c := New(Config{Addr: srv.URL + "/", Model: "m", Think: &off, KeepAlive: "5m0s", Temperature: tc.temperature, MaxTokens: tc.maxTokens})
			if _, err := c.Complete(context.Background(), "sys", "usr"); err != nil {
				t.Fatal(err)
			}
			if path != "/api/chat" {
				t.Fatalf("path = %s", path)
			}
			msgs, _ := got["messages"].([]any)
			if got["model"] != "m" || got["stream"] != false || got["think"] != false || got["keep_alive"] != "5m0s" || len(msgs) != 2 {
				t.Fatalf("payload = %v", got)
			}
			opts, _ := got["options"].(map[string]any)
			if len(opts) != len(tc.wantOptions) {
				t.Fatalf("options = %v, want %v", opts, tc.wantOptions)
			}
			for k, v := range tc.wantOptions {
				if opts[k] != v {
					t.Fatalf("options = %v, want %v", opts, tc.wantOptions)
				}
			}
			system, _ := msgs[0].(map[string]any)
			user, _ := msgs[1].(map[string]any)
			if system["role"] != "system" || system["content"] != "sys" || user["role"] != "user" || user["content"] != "usr" {
				t.Fatalf("messages = %v", msgs)
			}
		})
	}
}

func TestCompleteReturnsContent(t *testing.T) {
	srv := httptest.NewServer(reply("hello"))
	defer srv.Close()
	got, err := New(Config{Addr: srv.URL, Model: "m"}).Complete(context.Background(), "s", "u")
	if err != nil || got != "hello" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestHTTPContract(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"status error", http.StatusBadGateway, "unavailable", "status 502"},
		{"malformed json", http.StatusOK, `{`, "decode"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			_, err := New(Config{Addr: srv.URL, Model: "m", RequestTimeout: time.Second}).Complete(context.Background(), "s", "u")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
	// A body without a message decodes to empty content; callers own the
	// empty-output error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer srv.Close()
	if got, err := New(Config{Addr: srv.URL, Model: "m"}).Complete(context.Background(), "s", "u"); err != nil || got != "" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestThinkingControlPreservesExplicitFalseTrueAndModelDefault(t *testing.T) {
	for _, name := range []string{"default", "false", "true"} {
		t.Run(name, func(t *testing.T) {
			var think *bool
			if name != "default" {
				value := name == "true"
				think = &value
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode request: %v", err)
					return
				}
				got, present := request["think"]
				if name == "default" {
					if present {
						t.Errorf("think must be omitted for model default, got %s", got)
					}
				} else if !present || string(got) != name {
					t.Errorf("think = %s (present=%v), want %s", got, present, name)
				}
				reply("ok")(w, r)
			}))
			defer srv.Close()
			if _, err := New(Config{Addr: srv.URL, Model: "qwen3.5:4b", Think: think}).Complete(context.Background(), "s", "u"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHonorsTimeoutAndCancellation(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer srv.Close()

	c := New(Config{Addr: srv.URL, Model: "m", RequestTimeout: 10 * time.Millisecond})
	if _, err := c.Complete(context.Background(), "s", "u"); err == nil {
		t.Fatal("Complete() succeeded after client timeout")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := c.Complete(ctx, "s", "u")
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	if err := <-result; err == nil {
		t.Fatal("Complete() succeeded with canceled request")
	}
}
