package indexing

import (
	"context"

	"nadir/internal/core/embedding"
)

// Private seams belong to the consumer. Keeping these interfaces here makes
// the public indexing API small while allowing focused fakes in package tests.
type documentIndexer interface {
	GetAllFileSHAs(ctx context.Context) (map[string]string, error)
	ReplaceDocument(ctx context.Context, filePath, sourceSHA string, chunks []IndexedChunk) error
	DeleteDocument(ctx context.Context, filePath string) error
}

type cacheInvalidator interface {
	Clear(ctx context.Context) error
}

// A cache can suspend reuse throughout publication, independently from its
// best-effort persistence cleanup. Clear-only invalidators remain supported.
type cacheMutationGuard interface {
	BeginMutation() func()
}

type lifecycleCoordinator interface {
	BeginIngest(context.Context) error
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
