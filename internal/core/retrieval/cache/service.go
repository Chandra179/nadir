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
	// Invalidate logically before deleting records. A concurrent detached Set
	// may still finish after the delete, but its captured older generation will
	// never be accepted by Get.
	c.generation.Add(1)
	operationErr = c.backend.Clear(ctx)
	return operationErr
}

func (c *dependencies) Get(ctx context.Context, query string) ([]Candidate, bool, error) {
	if c.mutations.Load() > 0 {
		return nil, false, nil
	}
	version := c.cacheVersion()
	vec, err := c.embedder.Embed(ctx, c.embedQuery(query))
	if err != nil {
		return nil, false, fmt.Errorf("semantic cache embed: %w", err)
	}

	entry, hit, err := c.backend.Find(ctx, vec, c.threshold)
	if err != nil {
		return nil, false, fmt.Errorf("semantic cache search: %w", err)
	}
	if !hit {
		return nil, false, nil
	}

	if c.mutations.Load() > 0 || c.cacheVersion() != version || entry.Version != version {
		return nil, false, nil
	}
	if c.ttl > 0 {
		if !entry.CachedAt.IsZero() && time.Since(entry.CachedAt) > c.ttl {
			return nil, false, nil
		}
	}
	return entry.Results, true, nil
}

func (c *dependencies) Set(ctx context.Context, query string, candidates []Candidate) error {
	return c.PrepareWrite()(ctx, query, candidates)
}

// PrepareWrite binds a result write to the corpus generation that Retrieval
// will read. Capturing here, before Retrieval, also covers work whose detached
// callback does not begin until after an invalidation.
func (c *dependencies) PrepareWrite() func(context.Context, string, []Candidate) error {
	version := c.cacheVersion()
	if c.mutations.Load() > 0 {
		return func(context.Context, string, []Candidate) error { return nil }
	}
	return func(ctx context.Context, query string, candidates []Candidate) error {
		if c.mutations.Load() > 0 || c.cacheVersion() != version {
			return nil
		}
		vec, err := c.embedder.Embed(ctx, c.embedQuery(query))
		if err != nil {
			return fmt.Errorf("semantic cache embed for set: %w", err)
		}
		if c.mutations.Load() > 0 || c.cacheVersion() != version {
			return nil
		}

		// Clear can still overlap persistence; the record carries the older
		// generation and Get rejects it even if physical deletion failed.
		return c.backend.Put(ctx, query, vec, Entry{
			Version:  version,
			CachedAt: time.Now().UTC(),
			Results:  candidates,
		})
	}
}

func (c *dependencies) embedQuery(query string) string {
	return c.queryPrefix + query
}

func (c *dependencies) cacheVersion() string {
	return effectiveVersion(c.version, c.generation.Load())
}

func effectiveVersion(base string, generation uint64) string {
	return fmt.Sprintf("%s:g%d", base, generation)
}
