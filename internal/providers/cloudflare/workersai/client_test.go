package workersai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewRequiresAccountAndToken(t *testing.T) {
	if _, err := New(Config{APIToken: "t"}); err == nil || !strings.Contains(err.Error(), "account") {
		t.Fatalf("err = %v", err)
	}
	if _, err := New(Config{AccountID: "a"}); err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("err = %v", err)
	}
}

func TestRequestsGoUnderAIPrefix(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c, _ := New(Config{AccountID: "acct", APIToken: "t", BaseURL: srv.URL})
	if err := c.PostJSON(context.Background(), "/embeddings", 1, new(any)); err != nil {
		t.Fatal(err)
	}
	resp, err := c.Post(context.Background(), "/chat/completions", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	want := []string{"/accounts/acct/ai/v1/embeddings", "/accounts/acct/ai/v1/chat/completions"}
	if len(paths) != 2 || paths[0] != want[0] || paths[1] != want[1] {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}
