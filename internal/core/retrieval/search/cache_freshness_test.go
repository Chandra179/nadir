package search

import (
	"context"
	"testing"

	"nadir/internal/core/retrieval/cache"
)

type freshnessCacheBackend struct {
	entry cache.Entry
	hit   bool
}

func (b *freshnessCacheBackend) Find(context.Context, []float32, float32) (cache.Entry, bool, error) {
	return b.entry, b.hit, nil
}
func (b *freshnessCacheBackend) Put(_ context.Context, _ string, _ []float32, entry cache.Entry) error {
	b.entry, b.hit = entry, true
	return nil
}
func (b *freshnessCacheBackend) Clear(context.Context) error { b.hit = false; return nil }

func TestQueryRejectsDetachedCacheWriteAfterClear(t *testing.T) {
	ctx := context.Background()
	backend := &freshnessCacheBackend{}
	policy, err := cache.NewDependencies(cache.DependenciesConfig{Backend: backend, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	store := &searchTestStore{results: []SearchCandidate{{Text: "old corpus", FilePath: "a.md", Score: 1}}}
	var detached func(context.Context)
	retriever := NewDependencies(DependenciesConfig{
		Embedder: embTestEmbedder{}, Store: store, SemanticCache: policy,
		CacheWrite: func(_ context.Context, work func(context.Context)) bool { detached = work; return true },
	})
	if _, err := retriever.Query(ctx, Request{Query: "query", TopK: 1}); err != nil {
		t.Fatal(err)
	}
	if err := policy.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	store.results = []SearchCandidate{{Text: "new corpus", FilePath: "a.md", Score: 1}}
	detached(ctx)
	result, err := retriever.Query(ctx, Request{Query: "query", TopK: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.FromCache || result.Chunks[0].Text != "new corpus" {
		t.Fatalf("stale cache entry was accepted after Clear: %+v", result)
	}
}

func TestQueryDoesNotUseShortCacheEntryForLargerTopK(t *testing.T) {
	policy := &searchTestCache{hit: true, requestedTopK: 1, chunks: []cache.Candidate{{Text: "one", FilePath: "a.md"}}}
	store := &searchTestStore{results: []SearchCandidate{
		{Text: "one", FilePath: "a.md", Score: 1},
		{Text: "two", FilePath: "b.md", Score: 0.9},
	}}
	retriever := NewDependencies(DependenciesConfig{Embedder: embTestEmbedder{}, Store: store, SemanticCache: policy})
	result, err := retriever.Query(context.Background(), Request{Query: "query", TopK: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.FromCache || len(result.Chunks) != 2 || store.hybridCalls != 1 {
		t.Fatalf("short cache entry satisfied a larger result request: %+v", result)
	}
}

func TestQuerySkipCacheBypassesReadsAndWrites(t *testing.T) {
	policy := &searchTestCache{hit: true, chunks: []cache.Candidate{{Text: "cached"}}}
	store := &searchTestStore{results: []SearchCandidate{{Text: "fresh", FilePath: "a.md", Score: 1}}}
	submitted := false
	retriever := NewDependencies(DependenciesConfig{
		Embedder: embTestEmbedder{}, Store: store, SemanticCache: policy,
		CacheWrite: func(context.Context, func(context.Context)) bool { submitted = true; return true },
	})
	result, err := retriever.Query(context.Background(), Request{Query: "query", TopK: 1, SkipCache: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.FromCache || result.Chunks[0].Text != "fresh" || policy.getCalls != 0 || policy.prepareCalls != 0 || submitted {
		t.Fatalf("cache bypass performed cache work: result=%+v reads=%d writes=%d submitted=%v", result, policy.getCalls, policy.prepareCalls, submitted)
	}
}

func TestQueryTypedFusionBypassesQueryOnlyCache(t *testing.T) {
	policy := &searchTestCache{hit: true, chunks: []cache.Candidate{{Text: "auto ranking"}}}
	store := &searchTestStore{results: []SearchCandidate{{Text: "typed ranking", FilePath: "a.md", Score: 1}}}
	retriever := NewDependencies(DependenciesConfig{
		Embedder: embTestEmbedder{}, Store: store, SemanticCache: policy,
		Fusion: FusionConfig{Enabled: true},
	})
	result, err := retriever.Query(context.Background(), Request{Query: "query", TopK: 1, QueryType: QueryTypeFormula})
	if err != nil {
		t.Fatal(err)
	}
	if result.FromCache || policy.getCalls != 0 || policy.prepareCalls != 0 {
		t.Fatalf("typed fusion reused automatic-ranking cache: result=%+v reads=%d writes=%d", result, policy.getCalls, policy.prepareCalls)
	}
}

type clearingDocumentStore struct {
	searchTestStore
	clear func(context.Context) error
}

func (s *clearingDocumentStore) HybridSearch(ctx context.Context, vector []float32, query string, topK int, filter *Filter) (HybridSearchResult, error) {
	results, err := s.searchTestStore.HybridSearch(ctx, vector, query, topK, filter)
	if err != nil {
		return results, err
	}
	return results, s.clear(ctx)
}

func TestQueryCapturesCacheGenerationBeforeDocumentSearch(t *testing.T) {
	backend := &freshnessCacheBackend{}
	policy, err := cache.NewDependencies(cache.DependenciesConfig{Backend: backend})
	if err != nil {
		t.Fatal(err)
	}
	store := &clearingDocumentStore{
		searchTestStore: searchTestStore{results: []SearchCandidate{{Text: "before reset", FilePath: "a.md", Score: 1}}},
		clear:           policy.Clear,
	}
	retriever := NewDependencies(DependenciesConfig{Embedder: embTestEmbedder{}, Store: store, SemanticCache: policy})
	if _, err := retriever.Query(context.Background(), Request{Query: "query", TopK: 1}); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := policy.Get(context.Background(), []float32{1}); err != nil || hit {
		t.Fatalf("a search overlapping corpus invalidation cached its old results: hit=%v err=%v", hit, err)
	}
}
