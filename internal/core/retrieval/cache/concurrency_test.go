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
	policy, err := NewDependencies(DependenciesConfig{Backend: backend})
	if err != nil {
		t.Fatal(err)
	}
	write := policy.PrepareWrite()
	done := make(chan error, 1)
	go func() { done <- write(context.Background(), "query", testVector, []Candidate{{Text: "old"}}, 5) }()
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
	if _, hit, err := policy.Get(context.Background(), testVector); err != nil || hit {
		t.Fatalf("stale persistence completion became visible after Clear: hit=%v err=%v", hit, err)
	}
}

func TestSemanticCacheSuspendsDuringOverlappingCorpusMutations(t *testing.T) {
	backend := &cacheTestBackend{}
	policy, err := NewDependencies(DependenciesConfig{Backend: backend})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := policy.PrepareWrite()(ctx, "query", testVector, []Candidate{{Text: "old"}}, 5); err != nil {
		t.Fatal(err)
	}
	before := policy.PrepareWrite()
	first := policy.BeginMutation()
	during := policy.PrepareWrite()
	second := policy.BeginMutation()
	first()
	first() // Releasing one guard twice must not resume the cache early.
	if _, hit, err := policy.Get(ctx, testVector); err != nil || hit {
		t.Fatalf("cache resumed while another publication was active: hit=%v err=%v", hit, err)
	}
	second()
	for _, write := range []func(context.Context, string, []float32, []Candidate, int) error{before, during} {
		if err := write(ctx, "query", testVector, []Candidate{{Text: "stale"}}, 5); err != nil {
			t.Fatal(err)
		}
	}
	if _, hit, err := policy.Get(ctx, testVector); err != nil || hit {
		t.Fatalf("pre-publication or overlapping results survived publication: hit=%v err=%v", hit, err)
	}
	if err := policy.PrepareWrite()(ctx, "query", testVector, []Candidate{{Text: "new"}}, 5); err != nil {
		t.Fatal(err)
	}
	if result, hit, err := policy.Get(ctx, testVector); err != nil || !hit || result.Results[0].Text != "new" {
		t.Fatalf("cache did not resume with fresh results: result=%v hit=%v err=%v", result, hit, err)
	}
}
