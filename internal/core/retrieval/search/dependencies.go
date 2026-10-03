package search

import (
	"context"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"nadir/internal/core/embedding"
	"nadir/internal/core/observability"
	"nadir/internal/core/retrieval/cache"

	"log/slog"
)

// DependenciesConfig groups everything needed to construct the search
// dependencies.
type DependenciesConfig struct {
	Embedder embedding.Embedder
	Store    documentSearcher
	Reranker reranker
	// CandidateMul controls how many candidates are fetched before reranking.
	// It is ignored when Reranker is nil.
	CandidateMul            int
	AdaptiveRerank          bool
	AdaptiveMarginThreshold float32
	SemanticCache           cache.SemanticCache
	CacheWrite              func(context.Context, func(context.Context)) bool
	Log                     *slog.Logger
	// QueryPrefix is prepended to every embedded query fragment (e.g.
	// "search_query: " for nomic-embed-text task instructions).
	QueryPrefix            string
	MaxQueryChars          int
	MaxFragments           int
	MaxConcurrentFragments int
	MaxTopK                int
	MaxChunksPerFile       int
	Fusion                 FusionConfig
	Telemetry              *observability.Recorder
}

type dependencies struct {
	embedder                embedding.Embedder
	store                   documentSearcher
	reranker                reranker
	candidateMul            int
	adaptiveRerank          bool
	adaptiveMarginThreshold float32
	cache                   cache.SemanticCache
	cacheWrite              func(context.Context, func(context.Context)) bool
	queryPrefix             string
	maxQueryChars           int
	maxFragments            int
	maxConcurrentFragments  int
	maxTopK                 int
	maxChunksPerFile        int
	fusion                  FusionConfig
	telemetry               *observability.Recorder
	log                     *slog.Logger
}

var _ Retriever = (*dependencies)(nil)

// NewDependencies constructs the bounded hybrid Retrieval use case.
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
	adaptiveMarginThreshold := cfg.AdaptiveMarginThreshold
	if adaptiveMarginThreshold <= 0 {
		adaptiveMarginThreshold = 0.01
	}
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &dependencies{
		embedder:                cfg.Embedder,
		store:                   cfg.Store,
		reranker:                cfg.Reranker,
		candidateMul:            normalizeCandidateMul(cfg.CandidateMul),
		adaptiveRerank:          cfg.AdaptiveRerank,
		adaptiveMarginThreshold: adaptiveMarginThreshold,
		cache:                   cfg.SemanticCache,
		cacheWrite:              cfg.CacheWrite,
		queryPrefix:             cfg.QueryPrefix,
		maxQueryChars:           maxQueryChars,
		maxFragments:            maxFragments,
		maxConcurrentFragments:  maxConcurrentFragments,
		maxTopK:                 maxTopK,
		maxChunksPerFile:        maxChunksPerFile,
		fusion:                  normalizeFusionConfig(cfg.Fusion),
		telemetry:               cfg.Telemetry,
		log:                     log,
	}
}

func normalizeCandidateMul(candidateMul int) int {
	if candidateMul < 1 {
		return 3
	}
	return candidateMul
}

func normalizeFusionConfig(cfg FusionConfig) FusionConfig {
	if cfg.RRFK <= 0 {
		cfg.RRFK = 60
	}
	if cfg.DenseWeight <= 0 {
		cfg.DenseWeight = 1
	}
	if cfg.BM25Weight <= 0 {
		cfg.BM25Weight = 1
	}
	if cfg.MinExactTokens <= 0 {
		cfg.MinExactTokens = 1
	}
	if cfg.MinHeaderTokens <= 0 {
		cfg.MinHeaderTokens = 1
	}
	return cfg
}

// ErrQueryTooLong lets callers explain a rejected question without exposing
// arbitrary provider errors or depending on their text.
var ErrQueryTooLong = errors.New("search query exceeds the configured length limit")

func (s *dependencies) validateQuery(query string, topK int) error {
	if strings.TrimSpace(query) == "" {
		return errors.New("search query must not be empty")
	}
	if utf8.RuneCountInString(strings.TrimSpace(query)) > s.maxQueryChars {
		return ErrQueryTooLong
	}
	if topK <= 0 {
		return errors.New("search top_k must be greater than zero")
	}
	return nil
}
