package search

import (
	"context"

	"nadir/internal/embedding"
)

// These interfaces are consumer-owned dependency seams. They remain private
// because they are implementation details of the Retrieval use case, not
// public products of this Module.
type reranker interface {
	Rerank(ctx context.Context, query string, candidates []SearchCandidate) ([]SearchCandidate, error)
}

type documentSearcher interface {
	HybridSearch(ctx context.Context, vector []float32, query string, topK int, filter *Filter) ([]SearchCandidate, error)
	KeywordSearch(ctx context.Context, keyword string, topK int, filter *Filter) ([]SearchCandidate, error)
}

type batchEmbedder interface {
	embedding.Embedder
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}
