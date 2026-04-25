package pkb

import "context"

// Embedder converts text into a vector.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dimensions() int
}

// SparseEmbedder produces sparse vectors for indexing and querying.
// Implementations: SPLADESparseScorer (neural), TFSparseScorer (fallback).
type SparseEmbedder interface {
	// EmbedSparse returns sparse vector indices and values for the given text.
	// embedType is "query" or "passage".
	EmbedSparse(ctx context.Context, text, embedType string) (indices []uint32, values []float32, err error)
}
