package store

import (
	"fmt"

	"github.com/Chandra179/gosdk/logger"
	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const defaultPrefetchMul = 5

// DependenciesConfig groups everything needed to construct the Qdrant
// store.
type DependenciesConfig struct {
	Addr        string
	Collection  string
	PrefetchMul int
	Log         logger.Logger
}

// dependencies is a hybrid (dense + BM25) search store backed by Qdrant.
type dependencies struct {
	conn        *grpc.ClientConn
	points      qdrant.PointsClient
	collection  qdrant.CollectionsClient
	name        string
	prefetchMul int
	log         logger.Logger
}

var _ Store = (*dependencies)(nil)

func NewDependencies(cfg DependenciesConfig) (*dependencies, error) {
	prefetchMul := cfg.PrefetchMul
	if prefetchMul <= 0 {
		prefetchMul = defaultPrefetchMul
	}

	conn, err := grpc.NewClient(cfg.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("qdrant dial %s: %w", cfg.Addr, err)
	}
	return &dependencies{
		conn:        conn,
		points:      qdrant.NewPointsClient(conn),
		collection:  qdrant.NewCollectionsClient(conn),
		name:        cfg.Collection,
		prefetchMul: prefetchMul,
		log:         cfg.Log,
	}, nil
}
