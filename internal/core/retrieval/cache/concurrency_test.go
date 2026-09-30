package cache

import (
	"context"
	"sync"
	"testing"
	"time"
)

type blockingCacheBackend struct {
	mu      sync.Mutex
	entry   Entry
	hit     bool
	started chan struct{}
	release chan struct{}
}

func (b *blockingCacheBackend) Find(context.Context, []float32, float32) (Entry, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.entry, b.hit, nil
}

func (b *blockingCacheBackend) Put(_ context.Context, _ string, _ []float32, entry Entry) error {
	close(b.started)
	<-b.release
	b.mu.Lock()
	b.entry, b.hit = entry, true
	b.mu.Unlock()
	return nil
}

func (b *blockingCacheBackend) Clear(context.Context) error {
	b.mu.Lock()
	b.hit = false
	b.mu.Unlock()
	return nil
}

func TestSemanticCacheRejectsWriteAlreadyInPersistenceWhenCleared(t *testing.T) {
	backend := &blockingCacheBackend{started: make(chan struct{}), release: make(chan struct{})}
	policy, err := NewDependencies(DependenciesConfig{Backend: backend, Embedder: &cacheTestEmbedder{}})
	if err != nil {
		t.Fatal(err)
	}
	write := policy.PrepareWrite()
	done := make(chan error, 1)
	go func() { done <- write(context.Background(), "query", []Candidate{{Text: "old"}}) }()
	select {
	case <-backend.started:
	case <-time.After(time.Second):
		t.Fatal("cache write did not reach persistence")
	}
	if err := policy.Clear(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(backend.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, hit, err := policy.Get(context.Background(), "query"); err != nil || hit {
		t.Fatalf("stale persistence completion became visible after Clear: hit=%v err=%v", hit, err)
	}
}

type blockingCacheEmbedder struct {
	started chan struct{}
	release chan struct{}
}

func (e *blockingCacheEmbedder) Embed(context.Context, string) ([]float32, error) {
	close(e.started)
	<-e.release
	return []float32{1}, nil
}
func (e *blockingCacheEmbedder) Dimensions() int { return 1 }

func TestSemanticCacheDiscardsWriteWhenClearedDuringEmbedding(t *testing.T) {
	backend := &cacheTestBackend{}
	embedder := &blockingCacheEmbedder{started: make(chan struct{}), release: make(chan struct{})}
	policy, err := NewDependencies(DependenciesConfig{Backend: backend, Embedder: embedder})
	if err != nil {
		t.Fatal(err)
	}
	write := policy.PrepareWrite()
	done := make(chan error, 1)
	go func() { done <- write(context.Background(), "query", []Candidate{{Text: "old"}}) }()
	select {
	case <-embedder.started:
	case <-time.After(time.Second):
		t.Fatal("cache write did not reach embedding")
	}
	if err := policy.Clear(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(embedder.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if backend.hit {
		t.Fatal("obsolete write reached persistence after cache invalidation during embedding")
	}
}

func TestSemanticCacheSuspendsDuringOverlappingCorpusMutations(t *testing.T) {
	backend := &cacheTestBackend{}
	policy, err := NewDependencies(DependenciesConfig{Backend: backend, Embedder: &cacheTestEmbedder{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := policy.Set(ctx, "query", []Candidate{{Text: "old"}}); err != nil {
		t.Fatal(err)
	}
	before := policy.PrepareWrite()
	first := policy.BeginMutation()
	during := policy.PrepareWrite()
	second := policy.BeginMutation()
	first()
	first() // Releasing one guard twice must not resume the cache early.
	if _, hit, err := policy.Get(ctx, "query"); err != nil || hit {
		t.Fatalf("cache resumed while another publication was active: hit=%v err=%v", hit, err)
	}
	second()
	for _, write := range []func(context.Context, string, []Candidate) error{before, during} {
		if err := write(ctx, "query", []Candidate{{Text: "stale"}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, hit, err := policy.Get(ctx, "query"); err != nil || hit {
		t.Fatalf("pre-publication or overlapping results survived publication: hit=%v err=%v", hit, err)
	}
	if err := policy.Set(ctx, "query", []Candidate{{Text: "new"}}); err != nil {
		t.Fatal(err)
	}
	if result, hit, err := policy.Get(ctx, "query"); err != nil || !hit || result[0].Text != "new" {
		t.Fatalf("cache did not resume with fresh results: result=%v hit=%v err=%v", result, hit, err)
	}
}
