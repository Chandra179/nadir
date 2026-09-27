package cache

import "context"

// backend is the consumer-owned persistence seam for semantic-cache policy.
// Qdrant, Redis, and in-memory implementations remain replaceable.
type backend interface {
	Find(ctx context.Context, vector []float32, threshold float32) (Entry, bool, error)
	Put(ctx context.Context, query string, vector []float32, entry Entry) error
	Clear(ctx context.Context) error
}
