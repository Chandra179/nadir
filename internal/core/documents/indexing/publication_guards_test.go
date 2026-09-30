package indexing

import (
	"context"
	"errors"
	"testing"

	semanticcache "nadir/internal/core/retrieval/cache"
)

type publicationGuardBackend struct {
	entry semanticcache.Entry
}

func (b *publicationGuardBackend) Find(context.Context, []float32, float32) (semanticcache.Entry, bool, error) {
	return b.entry, len(b.entry.Results) > 0, nil
}
func (b *publicationGuardBackend) Put(_ context.Context, _ string, _ []float32, entry semanticcache.Entry) error {
	b.entry = entry
	return nil
}
func (*publicationGuardBackend) Clear(context.Context) error {
	return errors.New("cache persistence unavailable")
}

type guardedPublicationStore struct {
	fakeStore
	mutate func() error
}

func (s *guardedPublicationStore) ReplaceDocument(context.Context, string, string, []IndexedChunk) error {
	return s.mutate()
}
func (s *guardedPublicationStore) DeleteDocument(context.Context, string) error {
	return s.mutate()
}

// Exercise the actual cache policy at the Indexing composition seam. Backend
// invalidation deliberately fails, so freshness must come from mutation tokens.
func TestIndexingSuspendsCacheThroughoutEveryPublicationPath(t *testing.T) {
	for _, path := range []string{"replace", "reset", "mirror removal"} {
		for _, outcome := range []struct {
			name string
			err  error
		}{
			{"success", nil},
			{"published cleanup failure", &PublicationError{Err: errors.New("cleanup failed")}},
			{"unpublished failure", errors.New("storage failed")},
		} {
			t.Run(path+"/"+outcome.name, func(t *testing.T) {
				ctx := context.Background()
				policy, err := semanticcache.NewDependencies(semanticcache.DependenciesConfig{
					Backend: &publicationGuardBackend{}, Embedder: fakeEmbedder{},
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := policy.Set(ctx, "query", []semanticcache.Candidate{{Text: "old corpus"}}); err != nil {
					t.Fatal(err)
				}
				var delayed func(context.Context, string, []semanticcache.Candidate) error
				mutate := func() error {
					if _, hit, err := policy.Get(ctx, "query"); err != nil || hit {
						t.Errorf("cache returned stale corpus during %s: hit=%v err=%v", path, hit, err)
					}
					delayed = policy.PrepareWrite()
					return outcome.err
				}
				store := &guardedPublicationStore{mutate: mutate}
				indexer := NewDependencies(DependenciesConfig{
					Store: store, CacheInvalidator: policy, ClearCache: policy.Clear,
					Coordinator: NewDocumentLifecycle(), Reset: func(context.Context) error { return mutate() },
					Retry: RetryConfig{MaxAttempts: 1},
				})
				switch path {
				case "replace":
					_ = indexer.commitPlan(ctx, indexPlan{filePath: "a.md", sourceSHA: "new"})
				case "reset":
					_ = indexer.DeleteAll(ctx)
				case "mirror removal":
					_, _ = indexer.reconcileSources(ctx, nil, []string{"samples"}, map[string]string{"samples/a.md": "old"})
				}
				if delayed == nil {
					t.Fatal("mutation did not reach storage")
				}
				if err := delayed(ctx, "query", []semanticcache.Candidate{{Text: "overlapping results"}}); err != nil {
					t.Fatal(err)
				}
				if _, hit, err := policy.Get(ctx, "query"); err != nil || hit {
					t.Fatalf("cache accepted pre-publication or overlapping results after %s: hit=%v err=%v", path, hit, err)
				}
				if err := policy.Set(ctx, "query", []semanticcache.Candidate{{Text: "current corpus"}}); err != nil {
					t.Fatal(err)
				}
				if results, hit, err := policy.Get(ctx, "query"); err != nil || !hit || results[0].Text != "current corpus" {
					t.Fatalf("cache remained suspended after %s: results=%v hit=%v err=%v", path, results, hit, err)
				}
			})
		}
	}
}
