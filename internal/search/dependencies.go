package search

import (
	"nadir/internal/cache"
	"nadir/internal/embedder"
	"nadir/internal/reranker"
	"nadir/internal/store"

	"github.com/Chandra179/gosdk/logger"
)

// DependenciesConfig groups everything needed to construct the search
// dependencies.
type DependenciesConfig struct {
	Embedder embedder.Embedder
	Store    store.Store
	Log      logger.Logger
}

type Dependencies struct {
	embedder     embedder.Embedder
	store        store.Store
	reranker     reranker.Reranker
	candidateMul int
	cache        cache.Cache
	log          logger.Logger
}

func NewDependencies(cfg DependenciesConfig) *Dependencies {
	return &Dependencies{embedder: cfg.Embedder, store: cfg.Store, log: cfg.Log}
}

func (s *Dependencies) WithReranker(r reranker.Reranker, candidateMul int) *Dependencies {
	s.reranker = r
	if candidateMul < 1 {
		candidateMul = 3
	}
	s.candidateMul = candidateMul
	return s
}

// WithSemanticCache enables the semantic cache lookup/writeback performed by
// Query.
func (s *Dependencies) WithSemanticCache(c cache.Cache) *Dependencies {
	s.cache = c
	return s
}
