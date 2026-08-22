package search

import (
	"nadir/internal/cache"
	"nadir/internal/embedder"
	"nadir/internal/reranker"
	"nadir/internal/store"

	"go.uber.org/zap"
)

// DependenciesConfig groups everything needed to construct the search
// dependencies.
type DependenciesConfig struct {
	Embedder embedder.Embedder
	Store    store.Store
	Log      *zap.Logger
}

type dependencies struct {
	embedder     embedder.Embedder
	store        store.Store
	reranker     reranker.Reranker
	candidateMul int
	cache        cache.Cache
	log          *zap.Logger
}

func NewDependencies(cfg DependenciesConfig) *dependencies {
	return &dependencies{embedder: cfg.Embedder, store: cfg.Store, log: cfg.Log}
}

func (s *dependencies) WithReranker(r reranker.Reranker, candidateMul int) *dependencies {
	s.reranker = r
	if candidateMul < 1 {
		candidateMul = 3
	}
	s.candidateMul = candidateMul
	return s
}

// WithSemanticCache enables the semantic cache lookup/writeback performed by
// Query.
func (s *dependencies) WithSemanticCache(c cache.Cache) *dependencies {
	s.cache = c
	return s
}
