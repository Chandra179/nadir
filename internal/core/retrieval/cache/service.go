package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"nadir/internal/core/observability"
)

// BeginMutation suspends cache reuse while a Document publication is in flight.
// Each boundary invalidates outstanding read/write tokens, including searches
// which observed the old corpus during a mutation that later failed. Multiple
// concurrent file publications keep the cache suspended until all finish.
func (c *dependencies) BeginMutation() func() {
	c.mutations.Add(1)
	c.generation.Add(1)
	var once sync.Once
	return func() {
		once.Do(func() {
			c.generation.Add(1)
			c.mutations.Add(-1)
		})
	}
}

func (c *dependencies) Clear(ctx context.Context) error {
	ctx, operation := observability.Start(ctx, c.telemetry, nil, "cache_invalidation")
	var operationErr error
	defer func() {
		outcome := "success"
		if operationErr != nil {
			outcome = "error"
		}
		operation.End(outcome, operationErr)
	}()
	// Invalidate logically before deleting records. A concurrent detached write
	// may still finish after the delete, but its captured older generation will
	// never be accepted by Get.
	c.generation.Add(1)
	operationErr = c.backend.Clear(ctx)
	return operationErr
}

func (c *dependencies) Get(ctx context.Context, vector []float32) (Lookup, bool, error) {
	if c.mutations.Load() > 0 {
		return Lookup{}, false, nil
	}
	version := c.cacheVersion()
	entry, hit, err := c.backend.Find(ctx, vector, c.threshold)
	if err != nil {
		return Lookup{}, false, fmt.Errorf("semantic cache search: %w", err)
	}
	if !hit {
		return Lookup{}, false, nil
	}

	if c.mutations.Load() > 0 || c.cacheVersion() != version || entry.Version != version {
		return Lookup{}, false, nil
	}
	if c.ttl > 0 && !entry.CachedAt.IsZero() && time.Since(entry.CachedAt) > c.ttl {
		return Lookup{}, false, nil
	}
	return Lookup{Results: entry.Results, RequestedTopK: entry.RequestedTopK}, true, nil
}

// PrepareWrite binds a result write to the corpus generation that Retrieval
// will read. Capturing here, before Retrieval, also covers work whose detached
// callback does not begin until after an invalidation.
func (c *dependencies) PrepareWrite() func(context.Context, string, []float32, []Candidate, int) error {
	version := c.cacheVersion()
	if c.mutations.Load() > 0 {
		return func(context.Context, string, []float32, []Candidate, int) error { return nil }
	}
	return func(ctx context.Context, query string, vector []float32, candidates []Candidate, requestedTopK int) error {
		if c.mutations.Load() > 0 || c.cacheVersion() != version {
			return nil
		}

		// Clear can still overlap persistence; the record carries the older
		// generation and Get rejects it even if physical deletion failed.
		return c.backend.Put(ctx, query, vector, Entry{
			Version:       version,
			CachedAt:      time.Now().UTC(),
			Results:       candidates,
			RequestedTopK: requestedTopK,
		})
	}
}

func (c *dependencies) cacheVersion() string {
	return effectiveVersion(c.version, c.generation.Load())
}

func effectiveVersion(base string, generation uint64) string {
	return fmt.Sprintf("%s:g%d", base, generation)
}
