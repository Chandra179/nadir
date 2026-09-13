package indexing

import (
	"context"

	"nadir/internal/embedding"
)

// Private seams belong to the consumer. Keeping these interfaces here makes
// the public indexing API small while allowing focused fakes in package tests.
type documentIndexer interface {
	GetAllFileSHAs(ctx context.Context) (map[string]string, error)
	ReplaceDocument(ctx context.Context, filePath, sourceSHA string, chunks []IndexedChunk) error
}

type cacheInvalidator interface {
	Clear(ctx context.Context) error
}

type lifecycleCoordinator interface {
	BeginIngest()
	EndIngest()
	Reset(ctx context.Context, operation func(context.Context) error) error
}

type documentConverter interface {
	Convert(ctx context.Context, name string, data []byte) ([]byte, error)
}

type batchEmbedder interface {
	embedding.Embedder
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}
