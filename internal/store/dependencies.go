package store

import (
	qdrant "github.com/qdrant/go-client/qdrant"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

const defaultPrefetchMul = 5

// DependenciesConfig groups everything needed to construct the Qdrant
// store. Conn is a shared gRPC connection to Qdrant (the caller dials it
// once and reuses it across store/cache, rather than each opening its own).
type DependenciesConfig struct {
	Conn        *grpc.ClientConn
	Collection  string
	PrefetchMul int
	Log         *zap.Logger
}

// dependencies is a hybrid (dense + BM25) search store backed by Qdrant.
type dependencies struct {
	points      qdrant.PointsClient
	collection  qdrant.CollectionsClient
	name        string
	prefetchMul int
	log         *zap.Logger
}

var _ Store = (*dependencies)(nil)

func NewDependencies(cfg DependenciesConfig) (*dependencies, error) {
	prefetchMul := cfg.PrefetchMul
	if prefetchMul <= 0 {
		prefetchMul = defaultPrefetchMul
	}

	return &dependencies{
		points:      qdrant.NewPointsClient(cfg.Conn),
		collection:  qdrant.NewCollectionsClient(cfg.Conn),
		name:        cfg.Collection,
		prefetchMul: prefetchMul,
		log:         cfg.Log,
	}, nil
}
