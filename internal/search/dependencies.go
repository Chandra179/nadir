package search

import (
	"errors"
	"strings"

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
	QueryPrefix            string
	MaxQueryChars          int
	MaxFragments           int
	MaxConcurrentFragments int
	MaxTopK                int
	MaxChunksPerFile       int
}

type dependencies struct {
	embedder               embedder.Embedder
	store                  store.Store
	reranker               reranker.Reranker
	candidateMul           int
	cache                  cache.SemanticCache
	queryPrefix            string
	maxQueryChars          int
	maxFragments           int
	maxConcurrentFragments int
	maxTopK                int
	maxChunksPerFile       int
	log                    *zap.Logger
}

func NewDependencies(cfg DependenciesConfig) *dependencies {
	maxQueryChars := cfg.MaxQueryChars
	if maxQueryChars <= 0 {
		maxQueryChars = 8192
	}
	maxFragments := cfg.MaxFragments
	if maxFragments <= 0 {
		maxFragments = 16
	}
	maxConcurrentFragments := cfg.MaxConcurrentFragments
	if maxConcurrentFragments <= 0 {
		maxConcurrentFragments = 8
	}
	maxTopK := cfg.MaxTopK
	if maxTopK <= 0 {
		maxTopK = 50
	}
	maxChunksPerFile := cfg.MaxChunksPerFile
	if maxChunksPerFile <= 0 {
		maxChunksPerFile = 3
	}
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &dependencies{
		embedder:               cfg.Embedder,
		store:                  cfg.Store,
		queryPrefix:            cfg.QueryPrefix,
		maxQueryChars:          maxQueryChars,
		maxFragments:           maxFragments,
		maxConcurrentFragments: maxConcurrentFragments,
		maxTopK:                maxTopK,
		maxChunksPerFile:       maxChunksPerFile,
		log:                    log,
	}
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
func (s *dependencies) WithSemanticCache(c cache.SemanticCache) *dependencies {
	s.cache = c
	return s
}

var errQueryTooLong = errors.New("search query exceeds the configured length limit")

func (s *dependencies) validateQuery(query string, topK int) error {
	if strings.TrimSpace(query) == "" {
		return errors.New("search query must not be empty")
	}
	if len([]rune(strings.TrimSpace(query))) > s.maxQueryChars {
		return errQueryTooLong
	}
	if topK <= 0 {
		return errors.New("search top_k must be greater than zero")
	}
	return nil
}
