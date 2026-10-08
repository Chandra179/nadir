package cache

import "context"

// SemanticCache is the public Retrieval cache capability. Its value types are
// owned by this Module so persistence Adapters do not leak storage contracts
// into the search orchestration layer. Callers supply the query's dense
// vector: Retrieval embeds each question once and shares that vector with the
// cache lookup, the search and the write, so the cache never calls a model.
type SemanticCache interface {
	// Get returns the closest cached result set at or above the similarity
	// threshold. It misses while a corpus publication is in flight.
	Get(ctx context.Context, vector []float32) (Lookup, bool, error)
	// PrepareWrite captures corpus freshness before Retrieval starts. The
	// returned function remains safe when background execution is delayed
	// across invalidation; obsolete writes are discarded. requestedTopK is
	// the result count the search asked for, which may exceed len(results)
	// when the corpus or a per-document cap returned fewer.
	PrepareWrite() func(ctx context.Context, query string, vector []float32, results []Candidate, requestedTopK int) error
	Clear(ctx context.Context) error
}
