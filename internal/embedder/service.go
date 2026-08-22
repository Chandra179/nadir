package embedder

import "context"

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dimensions() int
}

type BatchEmbedder interface {
	Embedder
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}
