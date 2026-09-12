package eval

import (
	"context"

	"nadir/internal/search"
)

// Searcher is the Retrieval seam exercised by the evaluation Module.
// Implementations must return results in rank order and must honor
// search.Request.SkipCache so evaluation runs measure Retrieval itself.
type Searcher interface {
	Query(ctx context.Context, request search.Request) (search.Result, error)
}
