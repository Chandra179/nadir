package store

import "context"

type Store interface {
	Upsert(ctx context.Context, chunks []ScoredChunk) error
	DeleteByFile(ctx context.Context, filePath string) error
	// DeleteAll drops the collection and recreates it from scratch, so any
	// schema drift (e.g. a new vector field) is picked up as well.
	DeleteAll(ctx context.Context) error
	HybridSearch(ctx context.Context, vector []float32, query string, topK int, filter *SearchFilter) ([]ScoredChunk, error)
	KeywordSearch(ctx context.Context, keyword string, topK int, filter *SearchFilter) ([]ScoredChunk, error)
	GetAllFileSHAs(ctx context.Context) (map[string]string, error)
	Stats(ctx context.Context) (Stats, error)
}
