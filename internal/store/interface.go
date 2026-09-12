package store

import "context"

type Store interface {
	// ReplaceDocument stages a complete version of one Document before it is
	// made visible to Retrieval. An implementation must keep the previous
	// active version readable when staging fails.
	ReplaceDocument(ctx context.Context, filePath, sourceSHA string, chunks []ScoredChunk) error
	// DeleteAll publishes an empty, freshly provisioned collection generation
	// through the stable active alias, then retires old generations. A failed
	// provision or publication leaves the previous corpus addressable; cleanup
	// failures are returned as retryable errors after the new generation is live.
	DeleteAll(ctx context.Context) error
	HybridSearch(ctx context.Context, vector []float32, query string, topK int, filter *SearchFilter) ([]ScoredChunk, error)
	KeywordSearch(ctx context.Context, keyword string, topK int, filter *SearchFilter) ([]ScoredChunk, error)
	GetAllFileSHAs(ctx context.Context) (map[string]string, error)
	Stats(ctx context.Context) (Stats, error)
}
