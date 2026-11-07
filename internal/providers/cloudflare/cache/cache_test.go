package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	semanticcache "nadir/internal/core/retrieval/cache"
	"nadir/internal/providers/cloudflare/kv"
	"nadir/internal/providers/cloudflare/vectorize"
)

// fakeCloudflare is a stateful stand-in for the Vectorize and KV REST APIs:
// real cosine search, KV values with recorded TTLs and cursor-paged listing.
type fakeCloudflare struct {
	mu       sync.Mutex
	vectors  map[string][]float32
	values   map[string][]byte
	ttls     map[string]string // expiration_ttl query per key
	ops      []string          // "kv-put", "vec-upsert", ... in arrival order
	pageSize int
	dims     int
	metric   string
}

func newFake(t *testing.T) (*fakeCloudflare, *dependencies) {
	t.Helper()
	f := &fakeCloudflare{vectors: map[string][]float32{}, values: map[string][]byte{}, ttls: map[string]string{}, pageSize: 1000, dims: 3, metric: "cosine"}
	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)
	idx, err := vectorize.New(vectorize.Config{AccountID: "acct", APIToken: "tok", Index: "cache", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	store, err := kv.New(kv.Config{AccountID: "acct", APIToken: "tok", NamespaceID: "ns", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewDependencies(DependenciesConfig{Index: idx, Store: store, TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return f, d
}

func ok(w http.ResponseWriter, result any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "result": result})
}

func (f *fakeCloudflare) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	const idx = "/accounts/acct/vectorize/v2/indexes/cache"
	const ns = "/accounts/acct/storage/kv/namespaces/ns"
	path := r.URL.Path
	switch {
	case r.Method == "GET" && path == idx:
		ok(w, map[string]any{"config": map[string]any{"dimensions": f.dims, "metric": f.metric}})
	case path == idx+"/upsert":
		f.ops = append(f.ops, "vec-upsert")
		raw, _ := io.ReadAll(r.Body)
		var line struct {
			ID     string    `json:"id"`
			Values []float32 `json:"values"`
		}
		_ = json.Unmarshal(raw, &line)
		f.vectors[line.ID] = line.Values
		ok(w, map[string]any{"mutationId": "m"})
	case path == idx+"/query":
		var body struct {
			Vector []float32 `json:"vector"`
			TopK   int       `json:"topK"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		type match struct {
			ID    string  `json:"id"`
			Score float64 `json:"score"`
		}
		var matches []match
		for id, v := range f.vectors {
			matches = append(matches, match{id, cosine(body.Vector, v)})
		}
		sort.Slice(matches, func(i, j int) bool { return matches[i].Score > matches[j].Score })
		if len(matches) > body.TopK {
			matches = matches[:body.TopK]
		}
		ok(w, map[string]any{"count": len(matches), "matches": matches})
	case path == idx+"/delete_by_ids":
		f.ops = append(f.ops, "vec-delete")
		var body struct {
			IDs []string `json:"ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, id := range body.IDs {
			delete(f.vectors, id)
		}
		ok(w, map[string]any{})
	case r.Method == "PUT" && strings.HasPrefix(path, ns+"/values/"):
		f.ops = append(f.ops, "kv-put")
		key := strings.TrimPrefix(r.URL.Path, ns+"/values/")
		raw, _ := io.ReadAll(r.Body)
		f.values[key] = raw
		f.ttls[key] = r.URL.Query().Get("expiration_ttl")
		ok(w, nil)
	case r.Method == "GET" && strings.HasPrefix(path, ns+"/values/"):
		raw, found := f.values[strings.TrimPrefix(path, ns+"/values/")]
		if !found {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"errors":[{"message":"key not found"}]}`))
			return
		}
		_, _ = w.Write(raw)
	case r.Method == "GET" && path == ns+"/keys":
		prefix := r.URL.Query().Get("prefix")
		after := r.URL.Query().Get("cursor")
		var keys []string
		for k := range f.values {
			if strings.HasPrefix(k, strings.ReplaceAll(prefix, "/", "%2F")) || strings.HasPrefix(k, prefix) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		var page []map[string]string
		next := ""
		for _, k := range keys {
			if after != "" && k <= after {
				continue
			}
			if len(page) == f.pageSize {
				next = page[len(page)-1]["name"]
				break
			}
			page = append(page, map[string]string{"name": k})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": page, "result_info": map[string]string{"cursor": next}})
	case path == ns+"/bulk/delete":
		f.ops = append(f.ops, "kv-delete")
		var keys []string
		_ = json.NewDecoder(r.Body).Decode(&keys)
		for _, k := range keys {
			delete(f.values, k)
		}
		ok(w, nil)
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprintf(w, `{"errors":[{"message":"unexpected %s %s"}]}`, r.Method, path)
	}
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func entry(version string) semanticcache.Entry {
	return semanticcache.Entry{
		Version: version, CachedAt: time.Now().UTC().Truncate(time.Second), RequestedTopK: 5,
		Results: []semanticcache.Candidate{{Text: "chunk", FilePath: "a.md", Header: "H", LineStart: 3, ChunkIndex: 1, SourceSHA: "sha", Score: 0.8}},
	}
}

func TestEnsureIndexChecksDimensionsAndMetric(t *testing.T) {
	f, d := newFake(t)
	ctx := context.Background()
	if err := d.EnsureIndex(ctx, 3); err != nil {
		t.Fatal(err)
	}
	if err := d.EnsureIndex(ctx, 768); err == nil || !strings.Contains(err.Error(), "3 dimensions, want 768") {
		t.Fatalf("dimension err = %v", err)
	}
	f.metric = "euclidean"
	if err := d.EnsureIndex(ctx, 3); err == nil || !strings.Contains(err.Error(), "want cosine") {
		t.Fatalf("metric err = %v", err)
	}
}

func TestPutThenFindRoundTripsTheEntry(t *testing.T) {
	_, d := newFake(t)
	ctx := context.Background()
	want := entry("v1")
	if err := d.Put(ctx, "what is x?", []float32{1, 0, 0}, want); err != nil {
		t.Fatal(err)
	}
	got, hit, err := d.Find(ctx, []float32{1, 0, 0}, 0.9)
	if err != nil || !hit {
		t.Fatalf("hit=%v err=%v", hit, err)
	}
	if got.Version != "v1" || got.RequestedTopK != 5 || !got.CachedAt.Equal(want.CachedAt) || len(got.Results) != 1 || got.Results[0] != want.Results[0] {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestFindHonorsThresholdAndPicksNearest(t *testing.T) {
	_, d := newFake(t)
	ctx := context.Background()
	_ = d.Put(ctx, "x axis", []float32{1, 0, 0}, entry("x"))
	_ = d.Put(ctx, "y axis", []float32{0, 1, 0}, entry("y"))

	got, hit, _ := d.Find(ctx, []float32{0.1, 1, 0}, 0.9)
	if !hit || got.Version != "y" {
		t.Fatalf("nearest = %+v hit=%v, want the y entry", got, hit)
	}
	if _, hit, _ := d.Find(ctx, []float32{1, 1, 0}, 0.9); hit {
		t.Fatal("a query between both entries (cosine ~0.71) must miss at threshold 0.9")
	}
	if _, hit, _ := d.Find(ctx, []float32{1, 1, 0}, 0.7); !hit {
		t.Fatal("the same query must hit at threshold 0.7")
	}
}

func TestPutWritesKVBeforeTheVectorAndSetsTTL(t *testing.T) {
	f, d := newFake(t)
	if err := d.Put(context.Background(), "q", []float32{1, 0, 0}, entry("v")); err != nil {
		t.Fatal(err)
	}
	if len(f.ops) != 2 || f.ops[0] != "kv-put" || f.ops[1] != "vec-upsert" {
		t.Fatalf("ops = %v, want KV before the vector so a findable vector never lacks its entry", f.ops)
	}
	for _, ttl := range f.ttls {
		if ttl != "3600" {
			t.Fatalf("expiration_ttl = %q, want 3600", ttl)
		}
	}
	d.ttl = 0
	_ = d.Put(context.Background(), "q2", []float32{0, 1, 0}, entry("v"))
	if f.ttls[keyPrefix+entryID("q2")] != "" {
		t.Fatal("zero TTL must send no expiration")
	}
}

func TestSameQueryReplacesItsEntry(t *testing.T) {
	f, d := newFake(t)
	ctx := context.Background()
	_ = d.Put(ctx, "same", []float32{1, 0, 0}, entry("old"))
	_ = d.Put(ctx, "same", []float32{1, 0, 0}, entry("new"))
	if len(f.vectors) != 1 || len(f.values) != 1 {
		t.Fatalf("%d vectors, %d values; a repeated query must replace its entry", len(f.vectors), len(f.values))
	}
	got, hit, _ := d.Find(ctx, []float32{1, 0, 0}, 0.9)
	if !hit || got.Version != "new" {
		t.Fatalf("got %+v hit=%v", got, hit)
	}
}

func TestExpiredEntryIsAMissAndItsVectorIsRemoved(t *testing.T) {
	f, d := newFake(t)
	ctx := context.Background()
	_ = d.Put(ctx, "q", []float32{1, 0, 0}, entry("v"))
	for k := range f.values {
		delete(f.values, k) // KV expired the entry
	}
	if _, hit, err := d.Find(ctx, []float32{1, 0, 0}, 0.9); hit || err != nil {
		t.Fatalf("hit=%v err=%v, want a clean miss", hit, err)
	}
	if len(f.vectors) != 0 {
		t.Fatalf("%d orphaned vectors left; they would shadow valid neighbours", len(f.vectors))
	}
}

func TestClearRemovesEveryEntryAcrossPages(t *testing.T) {
	f, d := newFake(t)
	f.pageSize = 2
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		v := []float32{1, float32(i), 0}
		if err := d.Put(ctx, fmt.Sprintf("query %d", i), v, entry("v")); err != nil {
			t.Fatal(err)
		}
	}
	f.values["unrelated-key"] = []byte("keep") // outside the cache prefix
	if err := d.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.vectors) != 0 {
		t.Fatalf("%d vectors remain after Clear", len(f.vectors))
	}
	if len(f.values) != 1 || string(f.values["unrelated-key"]) != "keep" {
		t.Fatalf("KV after Clear = %v, want only the unrelated key", f.values)
	}
	if _, hit, _ := d.Find(ctx, []float32{1, 0, 0}, 0.5); hit {
		t.Fatal("Find hit after Clear")
	}
	if err := d.Clear(ctx); err != nil {
		t.Fatalf("clearing an empty cache: %v", err)
	}
}

func TestErrorsPropagate(t *testing.T) {
	d, err := NewDependencies(DependenciesConfig{})
	if d != nil || err == nil {
		t.Fatal("NewDependencies without an index and store must fail")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer srv.Close()
	idx, _ := vectorize.New(vectorize.Config{AccountID: "a", APIToken: "t", Index: "i", BaseURL: srv.URL})
	store, _ := kv.New(kv.Config{AccountID: "a", APIToken: "t", NamespaceID: "n", BaseURL: srv.URL})
	d, _ = NewDependencies(DependenciesConfig{Index: idx, Store: store})
	ctx := context.Background()
	if _, _, err := d.Find(ctx, []float32{1, 0, 0}, 0.9); err == nil || !strings.Contains(err.Error(), "semantic cache search") {
		t.Fatalf("find err = %v", err)
	}
	if err := d.Put(ctx, "q", []float32{1, 0, 0}, entry("v")); err == nil || !strings.Contains(err.Error(), "semantic cache write") {
		t.Fatalf("put err = %v", err)
	}
	if err := d.Clear(ctx); err == nil || !strings.Contains(err.Error(), "semantic cache clear") {
		t.Fatalf("clear err = %v", err)
	}
}

// The Adapter must satisfy the cache policy end to end: versions, generations
// and invalidation stay the policy's job.
func TestWorksUnderTheCachePolicy(t *testing.T) {
	_, d := newFake(t)
	policy, err := semanticcache.NewDependencies(semanticcache.DependenciesConfig{Backend: d, Threshold: 0.9, Version: "policy"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	vec := []float32{1, 0, 0}

	if _, hit, err := policy.Get(ctx, vec); hit || err != nil {
		t.Fatalf("cold cache: hit=%v err=%v", hit, err)
	}
	results := []semanticcache.Candidate{{Text: "chunk", FilePath: "a.md"}}
	if err := policy.PrepareWrite()(ctx, "q", vec, results, 5); err != nil {
		t.Fatal(err)
	}
	lookup, hit, err := policy.Get(ctx, vec)
	if err != nil || !hit || lookup.RequestedTopK != 5 || len(lookup.Results) != 1 {
		t.Fatalf("lookup=%+v hit=%v err=%v", lookup, hit, err)
	}

	end := policy.BeginMutation()
	if _, hit, _ := policy.Get(ctx, vec); hit {
		t.Fatal("a hit during a document mutation")
	}
	end()
	if _, hit, _ := policy.Get(ctx, vec); hit {
		t.Fatal("an entry from before the mutation must not hit after it")
	}

	_ = policy.PrepareWrite()(ctx, "q", vec, results, 5)
	if _, hit, _ := policy.Get(ctx, vec); !hit {
		t.Fatal("a fresh write after the mutation must hit")
	}
	if err := policy.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	if _, hit, _ := policy.Get(ctx, vec); hit {
		t.Fatal("hit after Clear")
	}
}
