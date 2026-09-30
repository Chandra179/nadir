package indexing

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

type publicationFailureStore struct {
	fakeStore
	errors []error
	calls  int
}

func (s *publicationFailureStore) ReplaceDocument(context.Context, string, string, []IndexedChunk) error {
	err := s.errors[s.calls]
	s.calls++
	return err
}

type publicationCache struct{ clears atomic.Int64 }

func (c *publicationCache) Clear(context.Context) error { c.clears.Add(1); return nil }

func TestIndexingInvalidatesAfterPublicationEvenWhenRetryFails(t *testing.T) {
	cleanupErr := errors.New("cleanup unavailable")
	store := &publicationFailureStore{errors: []error{
		&PublicationError{Err: cleanupErr},
		errors.New("read unavailable on retry"),
	}}
	cache := &publicationCache{}
	indexer := NewDependencies(DependenciesConfig{
		Chunker: &fakeChunker{}, Embedder: fakeEmbedder{}, Store: store,
		CacheInvalidator: cache, Retry: RetryConfig{MaxAttempts: 1},
	})
	result, err := indexer.Run(context.Background(), []UploadFile{{Name: "a.md", Data: []byte("new")}}, RunOptions{})
	if err != nil || result.Failed != 1 || result.Processed != 0 {
		t.Fatalf("indexing result=%+v err=%v, want one reported failure", result, err)
	}
	if cache.clears.Load() == 0 {
		t.Fatal("published replacement did not invalidate the cache after retries failed")
	}
}

func TestIndexingResetInvalidatesAfterPublishedCleanupFailure(t *testing.T) {
	cleanupErr := errors.New("retired collection delete unavailable")
	clearErr := errors.New("cache delete unavailable")
	cleared := false
	indexer := NewDependencies(DependenciesConfig{
		Coordinator: NewDocumentLifecycle(),
		Reset:       func(context.Context) error { return &PublicationError{Err: cleanupErr} },
		ClearCache:  func(context.Context) error { cleared = true; return clearErr },
	})
	err := indexer.DeleteAll(context.Background())
	if !cleared || !errors.Is(err, cleanupErr) || !errors.Is(err, clearErr) || !WasPublished(err) {
		t.Fatalf("reset outcome lost publication or cleanup errors: cleared=%v err=%v", cleared, err)
	}
}

func TestCommitPlanRetainsPublicationOutcomeAcrossRetries(t *testing.T) {
	finalErr := errors.New("read unavailable on retry")
	store := &publicationFailureStore{errors: []error{
		&PublicationError{Err: errors.New("cleanup unavailable")}, finalErr,
	}}
	indexer := NewDependencies(DependenciesConfig{Store: store, Retry: RetryConfig{MaxAttempts: 1}})
	err := indexer.commitPlan(context.Background(), indexPlan{filePath: "a.md", sourceSHA: "new"})
	if !WasPublished(err) || !errors.Is(err, finalErr) {
		t.Fatalf("final retry failure lost earlier publication outcome: %v", err)
	}
}
