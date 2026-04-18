package pkb

import "context"

// ScoredChunk is a retrieved chunk with its similarity score.
type ScoredChunk struct {
	DocumentChunk
	Vector    []float32 // populated during ingest; empty after retrieval
	SourceSHA string
	Score     float32 // populated by Store.Search; zero during ingest
}

// Store persists and retrieves chunk vectors.
type Store interface {
	// Upsert inserts or replaces chunks. Each ScoredChunk must have Vector set.
	Upsert(ctx context.Context, chunks []ScoredChunk) error
	// DeleteByFile removes all chunks belonging to a file path.
	DeleteByFile(ctx context.Context, filePath string) error
	// Search returns the top-k most similar chunks for a query vector.
	Search(ctx context.Context, vector []float32, topK int) ([]ScoredChunk, error)
	// HybridSearch combines dense vector search with BM25 full-text search via RRF.
	HybridSearch(ctx context.Context, vector []float32, query string, topK int) ([]ScoredChunk, error)
	// EnsureCollection creates the collection if it does not exist.
	EnsureCollection(ctx context.Context, dimensions int) error
	// GetFileSHA returns the stored source_sha for a file, or "" if not found.
	GetFileSHA(ctx context.Context, filePath string) (string, error)
}
