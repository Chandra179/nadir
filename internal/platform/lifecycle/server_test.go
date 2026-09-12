package server

import (
	"context"
	"testing"
	"time"
)

type compositionStore struct {
	deleteErr error
	deleted   bool
}

var _ documentResetter = (*compositionStore)(nil)

func (s *compositionStore) DeleteAll(context.Context) error {
	s.deleted = true
	return s.deleteErr
}

type compositionCache struct{ cleared bool }

var _ cacheClearer = (*compositionCache)(nil)

func (c *compositionCache) Clear(context.Context) error {
	c.cleared = true
	return nil
}

func TestCacheInvalidatingStoreClearsCacheAfterSuccessfulReset(t *testing.T) {
	base := &compositionStore{}
	cacheStore := &compositionCache{}
	decorated := &cacheInvalidatingStore{resetter: base, cache: cacheStore}
	if err := decorated.DeleteAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !base.deleted || !cacheStore.cleared {
		t.Fatalf("reset composition = store_deleted=%v cache_cleared=%v, want true/true", base.deleted, cacheStore.cleared)
	}
}

func TestCacheInvalidatingStoreDoesNotClearCacheAfterStoreFailure(t *testing.T) {
	base := &compositionStore{deleteErr: context.Canceled}
	cacheStore := &compositionCache{}
	decorated := &cacheInvalidatingStore{resetter: base, cache: cacheStore}
	if err := decorated.DeleteAll(context.Background()); err == nil {
		t.Fatal("DeleteAll() succeeded despite store failure")
	}
	if cacheStore.cleared {
		t.Fatal("cache was cleared after store reset failure")
	}
}

func TestCoordinatedStoreWaitsForIndexingBeforeReset(t *testing.T) {
	lifecycle := &documentLifecycle{}
	base := &compositionStore{}
	decorated := &coordinatedStore{resetter: base, lifecycle: lifecycle}
	lifecycle.BeginIngest()

	done := make(chan struct{})
	go func() {
		_ = decorated.DeleteAll(context.Background())
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("reset ran while indexing was active")
	case <-time.After(25 * time.Millisecond):
	}
	if base.deleted {
		t.Fatal("reset reached the store before indexing ended")
	}

	lifecycle.EndIngest()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reset did not run after indexing ended")
	}
	if !base.deleted {
		t.Fatal("reset did not reach the store")
	}
}
