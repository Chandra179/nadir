package reranker

import (
	"context"
	"nadir/internal/store"
)

type Reranker interface {
	Rerank(ctx context.Context, query string, chunks []store.ScoredChunk) ([]store.ScoredChunk, error)
}
