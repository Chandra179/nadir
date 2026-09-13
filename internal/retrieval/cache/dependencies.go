package cache

import (
	"fmt"
	"sync/atomic"
	"time"

	"nadir/internal/embedding"
)

const (
	defaultThreshold = 0.90
)

// DependenciesConfig groups everything needed to construct the semantic
// cache policy. Persistence is supplied through the private backend seam so this Module does
// not know which database stores its entries.
type DependenciesConfig struct {
	Backend     backend
	Embedder    embedding.Embedder
	Threshold   float32
	TTL         time.Duration
	QueryPrefix string
	Version     string
}

// dependencies applies semantic-cache policy over the injected persistence
// backend. The backend may be Qdrant, an in-memory store, Redis, or another
// implementation without changing this Module.
type dependencies struct {
	backend     backend
	embedder    embedding.Embedder
	threshold   float32
	ttl         time.Duration
	queryPrefix string
	version     string
	generation  atomic.Uint64
}

var _ SemanticCache = (*dependencies)(nil)

// NewDependencies constructs semantic-cache policy over an injected backend.
func NewDependencies(cfg DependenciesConfig) (*dependencies, error) {
	threshold := cfg.Threshold
	if threshold == 0 {
		threshold = defaultThreshold
	}
	if cfg.Backend == nil {
		return nil, fmt.Errorf("semantic cache backend is required")
	}
	if cfg.Embedder == nil {
		return nil, fmt.Errorf("semantic cache embedder is required")
	}
	return &dependencies{
		backend:     cfg.Backend,
		embedder:    cfg.Embedder,
		threshold:   threshold,
		ttl:         cfg.TTL,
		queryPrefix: cfg.QueryPrefix,
		version:     cfg.Version,
	}, nil
}
