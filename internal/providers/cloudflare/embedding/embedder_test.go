package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type fakeAPI struct {
	srv      *httptest.Server
	requests atomic.Int32
	dims     int
	reverse  bool
	status   int
}

func newFake(t *testing.T, dims int) *fakeAPI {
	t.Helper()
	f := &fakeAPI{dims: dims}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		if r.URL.Path != "/accounts/acct/ai/v1/embeddings" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("path=%s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		if f.status != 0 {
			w.WriteHeader(f.status)
			_, _ = w.Write([]byte(`{"errors":[{"message":"denied"}]}`))
			return
		}
		var req embeddingsRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		type item struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}
		var data []item
		for i, text := range req.Input {
			vec := make([]float32, f.dims)
			vec[0] = float32(len(text)) // identifies the input
			data = append(data, item{Index: i, Embedding: vec})
		}
		if f.reverse {
			for i, j := 0, len(data)-1; i < j; i, j = i+1, j-1 {
				data[i], data[j] = data[j], data[i]
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) embedder(t *testing.T, dims, batch int) *dependencies {
	t.Helper()
	e, err := NewDependencies(DependenciesConfig{AccountID: "acct", APIToken: "secret", Model: "@cf/m", Dimensions: dims, BaseURL: f.srv.URL, BatchSize: batch})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestNewDependenciesValidates(t *testing.T) {
	for name, cfg := range map[string]DependenciesConfig{
		"account":    {APIToken: "t", Model: "m", Dimensions: 3},
		"token":      {AccountID: "a", Model: "m", Dimensions: 3},
		"model":      {AccountID: "a", APIToken: "t", Dimensions: 3},
		"dimensions": {AccountID: "a", APIToken: "t", Model: "m"},
	} {
		if _, err := NewDependencies(cfg); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestEmbedRequestContractAndDimensions(t *testing.T) {
	f := newFake(t, 4)
	e := f.embedder(t, 4, 0)
	vec, err := e.Embed(context.Background(), "hello")
	if err != nil || len(vec) != 4 || vec[0] != 5 || e.Dimensions() != 4 {
		t.Fatalf("vec=%v err=%v", vec, err)
	}
}

func TestEmbedBatchSplitsAndKeepsOrder(t *testing.T) {
	f := newFake(t, 3)
	f.reverse = true // the adapter must honor the returned index, not the order
	e := f.embedder(t, 3, 2)
	texts := []string{"a", "bb", "ccc", "dddd", "eeeee"}
	vecs, err := e.EmbedBatch(context.Background(), texts)
	if err != nil || len(vecs) != 5 {
		t.Fatalf("vecs=%d err=%v", len(vecs), err)
	}
	for i, text := range texts {
		if vecs[i][0] != float32(len(text)) {
			t.Fatalf("vector %d is for another input: %v", i, vecs[i])
		}
	}
	if got := f.requests.Load(); got != 3 {
		t.Fatalf("requests = %d, want 3 (5 inputs, batch 2)", got)
	}
}

func TestEmbedRejectsWrongShape(t *testing.T) {
	f := newFake(t, 8)
	if _, err := f.embedder(t, 4, 0).Embed(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "returned 8 dimensions, want 4") {
		t.Fatalf("dimension mismatch err = %v", err)
	}

	short := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer short.Close()
	e, _ := NewDependencies(DependenciesConfig{AccountID: "a", APIToken: "t", Model: "m", Dimensions: 3, BaseURL: short.URL})
	if _, err := e.EmbedBatch(context.Background(), []string{"x", "y"}); err == nil || !strings.Contains(err.Error(), "got 0 embeddings for 2 inputs") {
		t.Fatalf("count mismatch err = %v", err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":[{"index":7,"embedding":[1,2,3]}]}`)
	}))
	defer bad.Close()
	e, _ = NewDependencies(DependenciesConfig{AccountID: "a", APIToken: "t", Model: "m", Dimensions: 3, BaseURL: bad.URL})
	if _, err := e.Embed(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "index 7") {
		t.Fatalf("index err = %v", err)
	}
}

func TestEmbedStatusErrorDoesNotLeakToken(t *testing.T) {
	f := newFake(t, 3)
	f.status = http.StatusForbidden
	_, err := f.embedder(t, 3, 0).Embed(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "status 403: denied") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err = %v", err)
	}
}

func TestEmptyBatchMakesNoRequest(t *testing.T) {
	f := newFake(t, 3)
	vecs, err := f.embedder(t, 3, 0).EmbedBatch(context.Background(), nil)
	if err != nil || len(vecs) != 0 || f.requests.Load() != 0 {
		t.Fatalf("vecs=%v err=%v requests=%d", vecs, err, f.requests.Load())
	}
}
