package kv

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Config{AccountID: "acct", APIToken: "secret", NamespaceID: "ns1", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewValidates(t *testing.T) {
	for name, cfg := range map[string]Config{
		"namespace": {AccountID: "a", APIToken: "t"},
		"account":   {APIToken: "t", NamespaceID: "n"},
		"token":     {AccountID: "a", NamespaceID: "n"},
	} {
		if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestPutSendsRawValueAndTTL(t *testing.T) {
	var method, path, query, body string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, query = r.Method, r.URL.EscapedPath(), r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{"success":true}`))
	})
	if err := c.Put(context.Background(), "cache/a b", []byte(`{"x":1}`), 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if method != "PUT" || path != "/accounts/acct/storage/kv/namespaces/ns1/values/cache%2Fa%20b" || query != "expiration_ttl=86400" || body != `{"x":1}` {
		t.Fatalf("method=%s path=%s query=%s body=%q", method, path, query, body)
	}
	if err := c.Put(context.Background(), "k", []byte("v"), time.Second); err != nil || query != "expiration_ttl=60" {
		t.Fatalf("a TTL below the minimum must be raised to 60s: query=%q err=%v", query, err)
	}
	if err := c.Put(context.Background(), "k", []byte("v"), 0); err != nil || query != "" {
		t.Fatalf("zero TTL must send no expiration: query=%q err=%v", query, err)
	}
}

func TestGetDistinguishesMissingFromErrors(t *testing.T) {
	status := http.StatusOK
	c := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte("stored bytes"))
		} else {
			_, _ = w.Write([]byte(`{"errors":[{"message":"nope"}]}`))
		}
	})
	got, found, err := c.Get(context.Background(), "k")
	if err != nil || !found || string(got) != "stored bytes" {
		t.Fatalf("got %q found=%v err=%v", got, found, err)
	}
	status = http.StatusNotFound
	if got, found, err := c.Get(context.Background(), "k"); err != nil || found || got != nil {
		t.Fatalf("missing key = %q found=%v err=%v", got, found, err)
	}
	status = http.StatusForbidden
	if _, _, err := c.Get(context.Background(), "k"); err == nil || !strings.Contains(err.Error(), "status 403") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err = %v", err)
	}
}

func TestListKeysPagesByCursor(t *testing.T) {
	var queries []string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if r.URL.Query().Get("cursor") == "" {
			_, _ = w.Write([]byte(`{"success":true,"result":[{"name":"cache/a"},{"name":"cache/b"}],"result_info":{"cursor":"next"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"result":[{"name":"cache/c"}],"result_info":{"cursor":""}}`))
	})
	first, cursor, err := c.ListKeys(context.Background(), "cache/", "")
	if err != nil || len(first) != 2 || cursor != "next" {
		t.Fatalf("first = %v cursor=%q err=%v", first, cursor, err)
	}
	second, cursor, err := c.ListKeys(context.Background(), "cache/", cursor)
	if err != nil || len(second) != 1 || second[0] != "cache/c" || cursor != "" {
		t.Fatalf("second = %v cursor=%q err=%v", second, cursor, err)
	}
	if !strings.Contains(queries[0], "prefix=cache%2F") || !strings.Contains(queries[0], "limit=1000") || !strings.Contains(queries[1], "cursor=next") {
		t.Fatalf("queries = %v", queries)
	}
}

func TestDeleteManySendsKeyArray(t *testing.T) {
	var calls [][]string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/accounts/acct/storage/kv/namespaces/ns1/bulk/delete" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		var keys []string
		_ = json.NewDecoder(r.Body).Decode(&keys)
		calls = append(calls, keys)
		_, _ = w.Write([]byte(`{"success":true}`))
	})
	if err := c.DeleteMany(context.Background(), []string{"a", "b"}); err != nil || len(calls) != 1 || len(calls[0]) != 2 {
		t.Fatalf("calls = %v err=%v", calls, err)
	}
	if err := c.DeleteMany(context.Background(), nil); err != nil || len(calls) != 1 {
		t.Fatalf("empty delete made a request: %v", calls)
	}
}
