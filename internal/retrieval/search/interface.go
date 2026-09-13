package search

import "context"

// Retriever is the public Retrieval use-case seam. It returns caller-facing
// search values and keeps storage-specific representations inside the
// Retrieval implementation.
type Retriever interface {
	Query(ctx context.Context, request Request) (Result, error)
}
