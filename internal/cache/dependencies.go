package cache

import (
	"fmt"
	"time"

	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"nadir/internal/embedder"
)

const (
	defaultCollection = "search_cache"
	defaultThreshold  = 0.90
)

// DependenciesConfig groups everything needed to construct the semantic
// cache.
type DependenciesConfig struct {
	Addr       string
	Collection string
	Embedder   embedder.Embedder
	Threshold  float32
	TTL        time.Duration
}

// dependencies is a semantic cache backed by a dedicated Qdrant collection.
type dependencies struct {
	conn       *grpc.ClientConn
	points     qdrant.PointsClient
	collection qdrant.CollectionsClient
	name       string
	embedder   embedder.Embedder
	threshold  float32
	ttl        time.Duration
	dimensions int
}

var _ Cache = (*dependencies)(nil)

func NewDependencies(cfg DependenciesConfig) (*dependencies, error) {
	collection := cfg.Collection
	if collection == "" {
		collection = defaultCollection
	}
	threshold := cfg.Threshold
	if threshold == 0 {
		threshold = defaultThreshold
	}

	conn, err := grpc.NewClient(cfg.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("semantic cache dial %s: %w", cfg.Addr, err)
	}
	return &dependencies{
		conn:       conn,
		points:     qdrant.NewPointsClient(conn),
		collection: qdrant.NewCollectionsClient(conn),
		name:       collection,
		embedder:   cfg.Embedder,
		threshold:  threshold,
		ttl:        cfg.TTL,
		dimensions: cfg.Embedder.Dimensions(),
	}, nil
}
