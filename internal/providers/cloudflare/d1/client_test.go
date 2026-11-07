package d1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func newClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Config{AccountID: "acct", APIToken: "secret", DatabaseID: "db1", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewValidates(t *testing.T) {
	for name, cfg := range map[string]Config{
		"database": {AccountID: "a", APIToken: "t"},
		"account":  {APIToken: "t", DatabaseID: "d"},
		"token":    {AccountID: "a", DatabaseID: "d"},
	} {
		if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestSingleStatementIsSentAsPlainQuery(t *testing.T) {
	var got map[string]any
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/accounts/acct/d1/database/db1/query" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("path=%s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":[{"success":true,"results":[{"id":"s1","n":1700000000123,"ok":1,"title":"hi"}]}]}`))
	})
	rows, err := c.Exec(context.Background(), Statement{SQL: "SELECT ?", Params: []any{"x", 2}})
	if err != nil {
		t.Fatal(err)
	}
	if got["sql"] != "SELECT ?" || got["batch"] != nil || len(got["params"].([]any)) != 2 {
		t.Fatalf("body = %v", got)
	}
	r := rows[0][0]
	if r.Str("id") != "s1" || r.Int("n") != 1700000000123 || !r.Bool("ok") || r.Str("missing") != "" || r.Int("missing") != 0 {
		t.Fatalf("row = %v", r)
	}
}

func TestMultipleStatementsAreSentAsBatch(t *testing.T) {
	var got struct {
		Batch []struct {
			SQL    string `json:"sql"`
			Params []any  `json:"params"`
		} `json:"batch"`
		SQL string `json:"sql"`
	}
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"success":true,"result":[{"success":true,"results":[]},{"success":true,"results":[{"a":"b"}]}]}`))
	})
	rows, err := c.Exec(context.Background(), Statement{SQL: "DELETE FROM t"}, Statement{SQL: "SELECT a", Params: []any{1}})
	if err != nil || len(rows) != 2 || rows[1][0].Str("a") != "b" {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	if got.SQL != "" || len(got.Batch) != 2 || got.Batch[0].SQL != "DELETE FROM t" || len(got.Batch[1].Params) != 1 {
		t.Fatalf("body = %+v", got)
	}
}

func TestErrors(t *testing.T) {
	var body = `{"success":false,"errors":[{"message":"bad sql"}],"result":[]}`
	status := http.StatusOK
	c := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	run := func() error { _, err := c.Exec(context.Background(), Statement{SQL: "x"}); return err }

	if err := run(); err == nil || !strings.Contains(err.Error(), "bad sql") {
		t.Fatalf("envelope err = %v", err)
	}
	body = `{"success":true,"result":[{"success":false,"error":"no such table"}]}`
	if err := run(); err == nil || !strings.Contains(err.Error(), "statement 1: no such table") {
		t.Fatalf("statement err = %v", err)
	}
	body = `{"success":true,"result":[]}`
	if err := run(); err == nil || !strings.Contains(err.Error(), "got 0 results for 1 statements") {
		t.Fatalf("count err = %v", err)
	}
	body, status = `{"errors":[{"message":"auth"}]}`, http.StatusForbidden
	err := run()
	if err == nil || !strings.Contains(err.Error(), "status 403: auth") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("status err = %v", err)
	}
	if rows, err := c.Exec(context.Background()); rows != nil || err != nil {
		t.Fatalf("no statements = %v, %v", rows, err)
	}
}

func TestOnlyRateLimitsAreRetried(t *testing.T) {
	var calls atomic.Int32
	status := http.StatusServiceUnavailable
	c := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(status)
	})
	if _, err := c.Exec(context.Background(), Statement{SQL: "INSERT"}); err == nil || calls.Load() != 1 {
		t.Fatalf("calls=%d, a 503 must not replay a write", calls.Load())
	}
}
