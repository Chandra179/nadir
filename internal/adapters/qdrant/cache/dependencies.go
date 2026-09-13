package cache

import (
	"fmt"

	qdrant "github.com/qdrant/go-client/qdrant"

	"nadir/internal/adapters/qdrant/shared"
)

const defaultCollection = "search_cache"

// DependenciesConfig groups the Qdrant resources used by the semantic-cache
// persistence Adapter. Collection provisioning remains an Adapter operation;
// cache policy is owned by internal/retrieval/cache.
type DependenciesConfig struct {
	Clients    qdrantutil.Clients
	Collection string
}

type dependencies struct {
	points     qdrant.PointsClient
	collection qdrant.CollectionsClient
	name       string
}

// NewDependencies constructs a semantic-cache persistence Adapter over the
// caller-owned shared Qdrant clients.
func NewDependencies(cfg DependenciesConfig) (*dependencies, error) {
	collection := cfg.Collection
	if collection == "" {
		collection = defaultCollection
	}
	if cfg.Clients.Points == nil || cfg.Clients.Collections == nil {
		return nil, fmt.Errorf("qdrant clients are required")
	}
	return &dependencies{
		points:     cfg.Clients.Points,
		collection: cfg.Clients.Collections,
		name:       collection,
	}, nil
}
