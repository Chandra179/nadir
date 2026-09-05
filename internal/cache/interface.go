package cache

import (
	"context"
	"nadir/internal/store"
)

type Cache interface {
	Get(ctx context.Context, query string) ([]store.ScoredChunk, bool, error)
	Set(ctx context.Context, query string, chunks []store.ScoredChunk) error
	Clear(ctx context.Context) error
	EnsureCollection(ctx context.Context) error
}
