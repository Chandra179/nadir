package server

import (
	"context"
	"testing"

	"nadir/internal/cache"
	"nadir/internal/store"
)

type compositionStore struct {
	store.Store
	deleteErr error
	deleted   bool
}

func (s *compositionStore) DeleteAll(context.Context) error {
	s.deleted = true
	return s.deleteErr
}

type compositionCache struct {
	cache.SemanticCache
	cleared bool
}

func (c *compositionCache) Clear(context.Context) error {
	c.cleared = true
	return nil
}

func TestCacheInvalidatingStoreClearsCacheAfterSuccessfulReset(t *testing.T) {
	base := &compositionStore{}
	cacheStore := &compositionCache{}
	decorated := &cacheInvalidatingStore{Store: base, cache: cacheStore}
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
	decorated := &cacheInvalidatingStore{Store: base, cache: cacheStore}
	if err := decorated.DeleteAll(context.Background()); err == nil {
		t.Fatal("DeleteAll() succeeded despite store failure")
	}
	if cacheStore.cleared {
		t.Fatal("cache was cleared after store reset failure")
	}
}
