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
	// QueryPrefix is prepended to every embedded query fragment (e.g.
	// "search_query: " for nomic-embed-text task instructions).
	QueryPrefix string
}

type dependencies struct {
	embedder     embedder.Embedder
	store        store.Store
	reranker     reranker.Reranker
	candidateMul int
	cache        cache.SemanticCache
	queryPrefix  string
	log          *zap.Logger
}

func NewDependencies(cfg DependenciesConfig) *dependencies {
	return &dependencies{embedder: cfg.Embedder, store: cfg.Store, queryPrefix: cfg.QueryPrefix, log: cfg.Log}
}

func (s *dependencies) WithReranker(r reranker.Reranker, candidateMul int) *dependencies {
	s.reranker = r
	if candidateMul < 1 {
		candidateMul = 3
	}
	s.candidateMul = candidateMul
	return s
}

// RerankerEnabled reports whether a reranker is wired in; used by eval
// tooling to label reports.
func (s *dependencies) RerankerEnabled() bool { return s.reranker != nil }

// WithSemanticCache enables the semantic cache lookup/writeback performed by
// Query.
func (s *dependencies) WithSemanticCache(c cache.SemanticCache) *dependencies {
	s.cache = c
	return s
}
