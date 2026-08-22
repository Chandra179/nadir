package generator

import (
	"context"
	"io"
	"nadir/internal/store"
)

type Generator interface {
	Generate(ctx context.Context, query string, chunks []store.ScoredChunk) (io.ReadCloser, error)
}
