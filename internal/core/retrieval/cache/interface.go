package cache

import "context"

// SemanticCache is the public Retrieval cache capability. Its value types are
// owned by this Module so persistence Adapters do not leak storage contracts
// into the search orchestration layer.
type SemanticCache interface {
	Get(ctx context.Context, query string) ([]Candidate, bool, error)
	Set(ctx context.Context, query string, candidates []Candidate) error
	Clear(ctx context.Context) error
}
