package cache

import (
	"context"
	"testing"
	"time"
)

var testVector = []float32{1, 2}

// write persists results through the same path Retrieval uses.
func write(t *testing.T, d *dependencies, query string, results []Candidate, requestedTopK int) {
	t.Helper()
	if err := d.PrepareWrite()(context.Background(), query, testVector, results, requestedTopK); err != nil {
		t.Fatal(err)
	}
}

type cacheTestBackend struct {
	entry      Entry
	hit        bool
	findVector []float32
	threshold  float32
	query      string
	putVector  []float32
	clearCalls int
}

func (b *cacheTestBackend) Find(_ context.Context, vector []float32, threshold float32) (Entry, bool, error) {
	b.findVector = append([]float32(nil), vector...)
	b.threshold = threshold
	return b.entry, b.hit, nil
}

func (b *cacheTestBackend) Put(_ context.Context, query string, vector []float32, entry Entry) error {
	b.query = query
	b.putVector = append([]float32(nil), vector...)
	b.entry = entry
	b.hit = true
	return nil
}

func (b *cacheTestBackend) Clear(context.Context) error {
	b.clearCalls++
	// Deliberately keep the entry: the policy must reject stale records by
	// generation even if a backend clear races with an in-flight write.
	return nil
}

var _ backend = (*cacheTestBackend)(nil)

func TestSemanticCacheDelegatesPersistenceAndAppliesQueryPolicy(t *testing.T) {
	backend := &cacheTestBackend{}
	d, err := NewDependencies(DependenciesConfig{
		Backend:   backend,
		Threshold: 0.87,
		Version:   "embed:v1",
	})
	if err != nil {
		t.Fatal(err)
	}

	write(t, d, "what is x?", []Candidate{{Text: "answer", Score: 0.9}}, 5)
	if backend.query != "what is x?" || backend.entry.Version != d.cacheVersion() || backend.entry.RequestedTopK != 5 {
		t.Fatalf("backend entry = %+v, query=%q; want current cache version and request size", backend.entry, backend.query)
	}
	if len(backend.putVector) != 2 || backend.putVector[0] != 1 {
		t.Fatalf("backend stored vector %v, want the caller's vector", backend.putVector)
	}

	got, hit, err := d.Get(context.Background(), testVector)
	if err != nil || !hit || len(got.Results) != 1 || got.Results[0].Text != "answer" || got.RequestedTopK != 5 {
		t.Fatalf("Get() = %+v, hit=%v, err=%v", got, hit, err)
	}
	if backend.threshold != 0.87 || len(backend.findVector) != 2 {
		t.Fatalf("backend Find inputs = threshold=%v vector=%v", backend.threshold, backend.findVector)
	}
}

func TestSemanticCacheClearRejectsStaleBackendEntries(t *testing.T) {
	backend := &cacheTestBackend{}
	d, err := NewDependencies(DependenciesConfig{
		Backend: backend,
		Version: "embed:v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	write(t, d, "query", []Candidate{{Text: "old"}}, 5)
	if err := d.Clear(context.Background()); err != nil {
		t.Fatal(err)
	}
	if backend.clearCalls != 1 {
		t.Fatalf("clear calls = %d, want 1", backend.clearCalls)
	}
	if _, hit, err := d.Get(context.Background(), testVector); err != nil {
		t.Fatal(err)
	} else if hit {
		t.Fatal("Get() returned a stale entry after cache invalidation")
	}
}

func TestSemanticCacheExpiresOldEntry(t *testing.T) {
	backend := &cacheTestBackend{
		hit: true,
		entry: Entry{
			Version:  "embed:v1:g0",
			CachedAt: time.Now().Add(-time.Hour),
			Results:  []Candidate{{Text: "old"}},
		},
	}
	d, err := NewDependencies(DependenciesConfig{
		Backend: backend,
		Version: "embed:v1",
		TTL:     time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	backend.entry.Version = d.cacheVersion()
	if _, hit, err := d.Get(context.Background(), testVector); err != nil {
		t.Fatal(err)
	} else if hit {
		t.Fatal("Get() returned an expired entry")
	}
}

func TestSemanticCacheRestartDoesNotRevivePreviousProcessEntry(t *testing.T) {
	backend := &cacheTestBackend{}
	config := DependenciesConfig{Backend: backend, Version: "embed:v1"}
	first, err := NewDependencies(config)
	if err != nil {
		t.Fatal(err)
	}
	write(t, first, "query", []Candidate{{Text: "previous corpus"}}, 5)
	restarted, err := NewDependencies(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, hit, err := restarted.Get(context.Background(), testVector); err != nil || hit {
		t.Fatalf("restarted cache revived previous process entry: hit=%v err=%v", hit, err)
	}
}

func TestSemanticCacheReturnsLegacyEntryWithoutRequestSize(t *testing.T) {
	backend := &cacheTestBackend{hit: true}
	d, err := NewDependencies(DependenciesConfig{Backend: backend, Version: "embed:v1"})
	if err != nil {
		t.Fatal(err)
	}
	backend.entry = Entry{Version: d.cacheVersion(), CachedAt: time.Now(), Results: []Candidate{{Text: "a"}, {Text: "b"}}}
	got, hit, err := d.Get(context.Background(), testVector)
	if err != nil || !hit || got.RequestedTopK != 0 || len(got.Results) != 2 {
		t.Fatalf("legacy entry = %+v hit=%v err=%v; callers must see the unset request size", got, hit, err)
	}
}
