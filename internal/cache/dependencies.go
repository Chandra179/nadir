package cache

import (
	"time"

	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"

	"nadir/internal/embedder"
)

const (
	defaultCollection = "search_cache"
	defaultThreshold  = 0.90
)

// DependenciesConfig groups everything needed to construct the semantic
// cache. Conn is a shared gRPC connection to Qdrant (the caller dials it
// once and reuses it across store/cache, rather than each opening its own).
type DependenciesConfig struct {
	Conn        *grpc.ClientConn
	Collection  string
	Embedder    embedder.Embedder
	Threshold   float32
	TTL         time.Duration
	QueryPrefix string
	Version     string
}

// dependencies is a semantic cache backed by a dedicated Qdrant collection.
type dependencies struct {
	points      qdrant.PointsClient
	collection  qdrant.CollectionsClient
	name        string
	embedder    embedder.Embedder
	threshold   float32
	ttl         time.Duration
	queryPrefix string
	version     string
	dimensions  int
}

var _ SemanticCache = (*dependencies)(nil)

func NewDependencies(cfg DependenciesConfig) (*dependencies, error) {
	collection := cfg.Collection
	if collection == "" {
		collection = defaultCollection
	}
	threshold := cfg.Threshold
	if threshold == 0 {
		threshold = defaultThreshold
	}

	return &dependencies{
		points:      qdrant.NewPointsClient(cfg.Conn),
		collection:  qdrant.NewCollectionsClient(cfg.Conn),
		name:        collection,
		embedder:    cfg.Embedder,
		threshold:   threshold,
		ttl:         cfg.TTL,
		queryPrefix: cfg.QueryPrefix,
		version:     cfg.Version,
		dimensions:  cfg.Embedder.Dimensions(),
	}, nil
}
