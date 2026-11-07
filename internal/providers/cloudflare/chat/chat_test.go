package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewValidates(t *testing.T) {
	for name, cfg := range map[string]Config{
		"account": {APIToken: "t", Model: "m"},
		"token":   {AccountID: "a", Model: "m"},
		"model":   {AccountID: "a", APIToken: "t"},
	} {
		if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

func TestCompleterRequestContract(t *testing.T) {
	var got request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/accounts/acct/ai/v1/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello"}}]}`))
	}))
	defer srv.Close()
	c, err := New(Config{AccountID: "acct", APIToken: "secret", Model: "@cf/m", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.Completer(64, 0.2)(context.Background(), "sys", "usr")
	if err != nil || out != "hello" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if got.Model != "@cf/m" || got.Stream || got.MaxTokens != 64 || got.Temperature != 0.2 ||
		len(got.Messages) != 2 || got.Messages[0].Role != "system" || got.Messages[1].Content != "usr" {
		t.Fatalf("request = %+v", got)
	}
}

func TestCompleterFailuresDoNotLeakToken(t *testing.T) {
	status := http.StatusInternalServerError
	body := `{"errors":[{"message":"boom"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c, _ := New(Config{AccountID: "a", APIToken: "secret", Model: "m", BaseURL: srv.URL})
	run := c.Completer(8, 0)

	_, err := run(context.Background(), "s", "u")
	if err == nil || !strings.Contains(err.Error(), "status 500: boom") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("status err = %v", err)
	}

	status, body = http.StatusOK, `{"choices":[{"message":{"content":"  "}}]}`
	if _, err := run(context.Background(), "s", "u"); err == nil || !strings.Contains(err.Error(), "empty completion") {
		t.Fatalf("empty err = %v", err)
	}
	status, body = http.StatusOK, `not json`
	if _, err := run(context.Background(), "s", "u"); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("decode err = %v", err)
	}
}
