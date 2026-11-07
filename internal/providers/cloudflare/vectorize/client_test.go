package vectorize

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Config{AccountID: "acct", APIToken: "secret", Index: "cache", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewValidates(t *testing.T) {
	for name, cfg := range map[string]Config{
		"index":   {AccountID: "a", APIToken: "t"},
		"account": {APIToken: "t", Index: "i"},
		"token":   {AccountID: "a", Index: "i"},
	} {
		if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestDescribe(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/accounts/acct/vectorize/v2/indexes/cache" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("%s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"success":true,"result":{"name":"cache","config":{"dimensions":768,"metric":"cosine"}}}`))
	})
	got, err := c.Describe(context.Background())
	if err != nil || got.Dimensions != 768 || got.Metric != "cosine" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestQuery(t *testing.T) {
	var body map[string]any
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/accounts/acct/vectorize/v2/indexes/cache/query" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("%s %s ct=%q", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"success":true,"result":{"count":2,"matches":[{"id":"a","score":0.97},{"id":"b","score":0.5}]}}`))
	})
	matches, err := c.Query(context.Background(), []float32{1, 0}, 2)
	if err != nil || len(matches) != 2 || matches[0].ID != "a" || matches[0].Score != 0.97 {
		t.Fatalf("matches = %+v, %v", matches, err)
	}
	if body["topK"] != float64(2) || body["returnValues"] != false || body["returnMetadata"] != "none" || len(body["vector"].([]any)) != 2 {
		t.Fatalf("body = %v", body)
	}
}

func TestUpsertSendsNDJSON(t *testing.T) {
	var raw, contentType string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
		if r.URL.Path != "/accounts/acct/vectorize/v2/indexes/cache/upsert" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"success":true,"result":{"mutationId":"m1"}}`))
	})
	if err := c.Upsert(context.Background(), "id-1", []float32{0.5, 1}); err != nil {
		t.Fatal(err)
	}
	if contentType != "application/x-ndjson" || !strings.HasSuffix(raw, "\n") || strings.Count(raw, "\n") != 1 {
		t.Fatalf("ct=%q body=%q", contentType, raw)
	}
	var line struct {
		ID     string    `json:"id"`
		Values []float32 `json:"values"`
	}
	if err := json.Unmarshal([]byte(raw), &line); err != nil || line.ID != "id-1" || len(line.Values) != 2 {
		t.Fatalf("line = %+v, %v", line, err)
	}
}

func TestDeleteByIDsBatchesAt1000(t *testing.T) {
	var sizes []int
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs []string `json:"ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		sizes = append(sizes, len(body.IDs))
		_, _ = w.Write([]byte(`{"success":true,"result":{}}`))
	})
	ids := make([]string, 2500)
	for i := range ids {
		ids[i] = "id"
	}
	if err := c.DeleteByIDs(context.Background(), ids); err != nil {
		t.Fatal(err)
	}
	if len(sizes) != 3 || sizes[0] != 1000 || sizes[1] != 1000 || sizes[2] != 500 {
		t.Fatalf("batch sizes = %v", sizes)
	}
	if err := c.DeleteByIDs(context.Background(), nil); err != nil || len(sizes) != 3 {
		t.Fatalf("empty delete made a request: %v", sizes)
	}
}

func TestErrors(t *testing.T) {
	status, body := http.StatusOK, `{"success":false,"errors":[{"message":"bad dimensions"}]}`
	c := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	if _, err := c.Query(context.Background(), []float32{1}, 1); err == nil || !strings.Contains(err.Error(), "bad dimensions") {
		t.Fatalf("envelope err = %v", err)
	}
	body, status = `{"errors":[{"message":"no auth"}]}`, http.StatusForbidden
	err := c.Upsert(context.Background(), "x", []float32{1})
	if err == nil || !strings.Contains(err.Error(), "status 403: no auth") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("status err = %v", err)
	}
	body, status = `not json`, http.StatusOK
	if _, err := c.Describe(context.Background()); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("decode err = %v", err)
	}
}
