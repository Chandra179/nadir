package cfapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewRequiresAccountAndToken(t *testing.T) {
	if _, err := New(Config{APIToken: "t"}); err == nil || !strings.Contains(err.Error(), "account") {
		t.Fatalf("err = %v", err)
	}
	if _, err := New(Config{AccountID: "a"}); err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("err = %v", err)
	}
}

func TestPostJSONContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/accounts/acct/ai/v1/embeddings" || r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("path=%s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c, _ := New(Config{AccountID: "acct", APIToken: "secret", BaseURL: srv.URL})
	var out struct{ OK bool }
	if err := c.PostJSON(context.Background(), "/ai/v1/embeddings", map[string]string{"a": "b"}, &out); err != nil || !out.OK {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestStatusErrorCarriesDetailWithoutToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":[{"message":"no access"}]}`))
	}))
	defer srv.Close()
	c, _ := New(Config{AccountID: "a", APIToken: "secret", BaseURL: srv.URL})
	err := c.PostJSON(context.Background(), "/x", 1, new(any))
	var se *StatusError
	if !errors.As(err, &se) || se.Code != 403 || se.Detail != "no access" || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err = %v", err)
	}
	if err.Error() != "status 403: no access" {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestRetriesRateLimitThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c, _ := New(Config{AccountID: "a", APIToken: "t", BaseURL: srv.URL, RetryDelay: time.Millisecond})
	if err := c.PostJSON(context.Background(), "/x", 1, new(any)); err != nil || calls.Load() != 3 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
}

func TestRetryStopsAtLimitAndSkipsOtherStatuses(t *testing.T) {
	var calls atomic.Int32
	status := http.StatusTooManyRequests
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(status)
	}))
	defer srv.Close()
	c, _ := New(Config{AccountID: "a", APIToken: "t", BaseURL: srv.URL, RetryDelay: time.Millisecond})
	if err := c.PostJSON(context.Background(), "/x", 1, new(any)); err == nil || calls.Load() != 3 {
		t.Fatalf("calls=%d err=%v, want 3 attempts then error", calls.Load(), err)
	}
	calls.Store(0)
	status = http.StatusInternalServerError
	if err := c.PostJSON(context.Background(), "/x", 1, new(any)); err == nil || calls.Load() != 1 {
		t.Fatalf("calls=%d, want 1 (500 is not retried)", calls.Load())
	}
	calls.Store(0)
	status = http.StatusTooManyRequests
	off, _ := New(Config{AccountID: "a", APIToken: "t", BaseURL: srv.URL, MaxRetries: -1})
	if err := off.PostJSON(context.Background(), "/x", 1, new(any)); err == nil || calls.Load() != 1 {
		t.Fatalf("calls=%d, want 1 with retries disabled", calls.Load())
	}
}

func TestRetryHonorsRetryAfterAndCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c, _ := New(Config{AccountID: "a", APIToken: "t", BaseURL: srv.URL})
	if got := c.backoff(0, "5"); got != 5*time.Second {
		t.Fatalf("backoff = %v", got)
	}
	if got := c.backoff(0, "999"); got != maxRetryAfter {
		t.Fatalf("backoff cap = %v", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := c.PostJSON(ctx, "/x", 1, new(any)); err == nil || time.Since(start) > time.Second {
		t.Fatalf("err=%v after %v, want prompt cancellation during Retry-After wait", err, time.Since(start))
	}
}

func TestRetryStatusesAreConfigurable(t *testing.T) {
	var calls atomic.Int32
	status := http.StatusServiceUnavailable
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(status)
	}))
	defer srv.Close()
	only429, _ := New(Config{AccountID: "a", APIToken: "t", BaseURL: srv.URL, RetryDelay: time.Millisecond, RetryStatuses: []int{http.StatusTooManyRequests}})
	if err := only429.PostJSON(context.Background(), "/x", 1, new(any)); err == nil || calls.Load() != 1 {
		t.Fatalf("calls=%d, want 1: 503 must not be retried for writes", calls.Load())
	}
	calls.Store(0)
	status = http.StatusTooManyRequests
	if err := only429.PostJSON(context.Background(), "/x", 1, new(any)); err == nil || calls.Load() != 3 {
		t.Fatalf("calls=%d, want 3: 429 is retried", calls.Load())
	}
}
