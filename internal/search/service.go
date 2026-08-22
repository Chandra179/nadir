package search

import (
	"context"

	"nadir/internal/store"
)

// Search is the top-level entry point for a search request: it dispatches to
// keyword or semantic search and, for semantic queries, transparently
// consults the semantic cache before searching and writes back on miss.
type Search interface {
	Query(ctx context.Context, query, keyword string, topK int, filter *store.SearchFilter, skipCache bool) (chunks []store.ScoredChunk, fromCache bool, err error)
}
