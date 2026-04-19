package pkb

import "context"

// Embedder converts text into a vector.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dimensions() int
}

// SparseScorer scores a chunk against a query for the BM25 leg of hybrid search.
// Implementations can use TF-proxy (default), SPLADE, or any other sparse method.
type SparseScorer interface {
	Score(query, text string) float64
}
