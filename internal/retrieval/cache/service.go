package cache

import (
	"context"
	"fmt"
	"time"
)

func (c *dependencies) Clear(ctx context.Context) error {
	// Invalidate logically before deleting records. A concurrent detached Set
	// may still finish after the delete, but its captured older generation will
	// never be accepted by Get.
	c.generation.Add(1)
	return c.backend.Clear(ctx)
}

func (c *dependencies) Get(ctx context.Context, query string) ([]Candidate, bool, error) {
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

	if c.cacheVersion() != version || entry.Version != version {
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
	version := c.cacheVersion()
	vec, err := c.embedder.Embed(ctx, c.embedQuery(query))
	if err != nil {
		return fmt.Errorf("semantic cache embed for set: %w", err)
	}

	return c.backend.Put(ctx, query, vec, Entry{
		Version:  version,
		CachedAt: time.Now().UTC(),
		Results:  candidates,
	})
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
