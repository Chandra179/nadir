package reranker

import (
	"context"

	"nadir/internal/adapters/qdrant/documents"
)

type Reranker interface {
	Rerank(ctx context.Context, query string, chunks []store.ScoredChunk) ([]store.ScoredChunk, error)
}
