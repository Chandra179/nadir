package cache

import (
	"context"
	"errors"
	"testing"
	"time"
)

type cacheTestEmbedder struct {
	inputs []string
	err    error
}

func (e *cacheTestEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	e.inputs = append(e.inputs, text)
	if e.err != nil {
		return nil, e.err
	}
	return []float32{1, 2}, nil
}

func (e *cacheTestEmbedder) Dimensions() int { return 2 }

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
	embedder := &cacheTestEmbedder{}
	d, err := NewDependencies(DependenciesConfig{
		Backend:     backend,
		Embedder:    embedder,
		Threshold:   0.87,
		QueryPrefix: "query: ",
		Version:     "embed:v1",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []Candidate{{Text: "answer", Score: 0.9}}
	if err := d.Set(context.Background(), "what is x?", want); err != nil {
		t.Fatal(err)
	}
	if backend.query != "what is x?" || backend.entry.Version != "embed:v1:g0" {
		t.Fatalf("backend entry = %+v, query=%q; want version embed:v1:g0", backend.entry, backend.query)
	}
	if len(embedder.inputs) != 1 || embedder.inputs[0] != "query: what is x?" {
		t.Fatalf("embed inputs = %q, want prefixed query", embedder.inputs)
	}

	got, hit, err := d.Get(context.Background(), "what is x?")
	if err != nil || !hit || len(got) != 1 || got[0].Text != "answer" {
		t.Fatalf("Get() = %+v, hit=%v, err=%v", got, hit, err)
	}
	if backend.threshold != 0.87 || len(backend.findVector) != 2 {
		t.Fatalf("backend Find inputs = threshold=%v vector=%v", backend.threshold, backend.findVector)
	}
}

func TestSemanticCacheClearRejectsStaleBackendEntries(t *testing.T) {
	backend := &cacheTestBackend{}
	d, err := NewDependencies(DependenciesConfig{
		Backend:  backend,
		Embedder: &cacheTestEmbedder{},
		Version:  "embed:v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Set(context.Background(), "query", []Candidate{{Text: "old"}}); err != nil {
		t.Fatal(err)
	}
	if err := d.Clear(context.Background()); err != nil {
		t.Fatal(err)
	}
	if backend.clearCalls != 1 {
		t.Fatalf("clear calls = %d, want 1", backend.clearCalls)
	}
	if _, hit, err := d.Get(context.Background(), "query"); err != nil {
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
		Backend:  backend,
		Embedder: &cacheTestEmbedder{},
		Version:  "embed:v1",
		TTL:      time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, hit, err := d.Get(context.Background(), "query"); err != nil {
		t.Fatal(err)
	} else if hit {
		t.Fatal("Get() returned an expired entry")
	}
}

func TestSemanticCacheWrapsEmbedderErrors(t *testing.T) {
	d, err := NewDependencies(DependenciesConfig{
		Backend:  &cacheTestBackend{},
		Embedder: &cacheTestEmbedder{err: errors.New("provider down")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.Get(context.Background(), "query"); err == nil {
		t.Fatal("Get() succeeded despite embedder failure")
	}
}
