package store

import (
	"fmt"
	"sync"

	qdrant "github.com/qdrant/go-client/qdrant"

	"nadir/internal/providers/qdrant/shared"
)

const defaultPrefetchMul = 5

// DependenciesConfig groups everything needed to construct the Qdrant
// store. Conn is a shared gRPC connection to Qdrant (the caller dials it
// once and reuses it across store/cache, rather than each opening its own).
type DependenciesConfig struct {
	Clients         qdrantutil.Clients
	Collection      string
	PrefetchMul     int
	AdaptiveSignals bool
	HNSW            HNSWConfig
}

// HNSWConfig tunes the dense vector index. Zero values keep Qdrant's server
// defaults. Build parameters (M, EfConstruct, FullScanThreshold,
// IndexingThreshold) apply only when a collection is created; Ef applies to
// every dense query.
type HNSWConfig struct {
	M                 int
	EfConstruct       int
	FullScanThreshold int // KB
	IndexingThreshold int // KB
	Ef                int
}

// dependencies is a hybrid (dense + BM25) search store backed by Qdrant.
type dependencies struct {
	points          qdrant.PointsClient
	collection      qdrant.CollectionsClient
	name            string
	activeAlias     string
	prefetchMul     int
	adaptiveSignals bool
	hnsw            HNSWConfig
	dimensions      int
	mu              sync.RWMutex
	publicationMu   sync.Mutex
}

// NewDependencies constructs a document persistence Adapter over shared
// Qdrant clients.
func NewDependencies(cfg DependenciesConfig) (*dependencies, error) {
	prefetchMul := cfg.PrefetchMul
	if prefetchMul <= 0 {
		prefetchMul = defaultPrefetchMul
	}

	if cfg.Clients.Points == nil || cfg.Clients.Collections == nil {
		return nil, fmt.Errorf("qdrant clients are required")
	}
	return &dependencies{
		points:          cfg.Clients.Points,
		collection:      cfg.Clients.Collections,
		name:            cfg.Collection,
		activeAlias:     activeAliasName(cfg.Collection),
		prefetchMul:     prefetchMul,
		adaptiveSignals: cfg.AdaptiveSignals,
		hnsw:            cfg.HNSW,
	}, nil
}
