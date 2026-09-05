package generator

import (
	"context"
	"io"

	"nadir/internal/store"
)

type Generator interface {
	// Generate also returns the exact prompt (instructions + numbered
	// context) sent to the LLM, so callers can surface it — e.g. the chat
	// UI's "Think" trace — without rebuilding or duplicating that logic.
	Generate(ctx context.Context, query string, chunks []store.ScoredChunk) (prompt string, r io.ReadCloser, err error)
}
