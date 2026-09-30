package cache

import "context"

// SemanticCache is the public Retrieval cache capability. Its value types are
// owned by this Module so persistence Adapters do not leak storage contracts
// into the search orchestration layer.
type SemanticCache interface {
	Get(ctx context.Context, query string) ([]Candidate, bool, error)
	// PrepareWrite captures corpus freshness before Retrieval starts. The
	// returned function remains safe when background execution is delayed
	// across invalidation; obsolete writes are discarded.
	PrepareWrite() func(context.Context, string, []Candidate) error
	Clear(ctx context.Context) error
}
