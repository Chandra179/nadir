package search

import (
	"context"

	"nadir/internal/adapters/qdrant/documents"
)

// Retriever is the public Retrieval use-case seam. It returns caller-facing
// search values and keeps storage-specific representations inside the
// Retrieval implementation.
type Retriever interface {
	Query(ctx context.Context, request Request) (Result, error)
}

// documentSearcher is the narrow Document Store capability needed by
// Retrieval. Administrative reset, indexing, and statistics do not belong on
// this seam.
type documentSearcher interface {
	HybridSearch(ctx context.Context, vector []float32, query string, topK int, filter *store.SearchFilter) ([]store.ScoredChunk, error)
	KeywordSearch(ctx context.Context, keyword string, topK int, filter *store.SearchFilter) ([]store.ScoredChunk, error)
}
