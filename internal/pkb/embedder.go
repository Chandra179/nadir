package pkb

import "context"

// Embedder converts text into a vector.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dimensions() int
}
