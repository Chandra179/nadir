package eval

import (
	"context"

	"nadir/internal/store"
)

// Searcher mirrors the search service's Query entry point; defined here so
// the harness depends on an interface instead of the search package.
type Searcher interface {
	Query(ctx context.Context, query, keyword string, topK int, filter *store.SearchFilter, skipCache bool) (chunks []store.ScoredChunk, fromCache bool, err error)
}