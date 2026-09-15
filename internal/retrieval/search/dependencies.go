package search

import (
	"context"
	"errors"
	"strings"

	"nadir/internal/embedding"
	"nadir/internal/platform/observability"
	"nadir/internal/retrieval/cache"

	"go.uber.org/zap"
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
	Log                     *zap.Logger
	// QueryPrefix is prepended to every embedded query fragment (e.g.
	// "search_query: " for nomic-embed-text task instructions).
	QueryPrefix            string
	MaxQueryChars          int
	MaxFragments           int
	MaxConcurrentFragments int
	MaxTopK                int
	MaxChunksPerFile       int
	Fusion                 FusionConfig
	FragmentAdmission      func(context.Context) (func(), error)
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
	queryPrefix             string
	maxQueryChars           int
	maxFragments            int
	maxConcurrentFragments  int
	maxTopK                 int
	maxChunksPerFile        int
	fusion                  FusionConfig
	fragmentAdmission       func(context.Context) (func(), error)
	telemetry               *observability.Recorder
	log                     *zap.Logger
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
		log = zap.NewNop()
	}
	return &dependencies{
		embedder:                cfg.Embedder,
		store:                   cfg.Store,
		reranker:                cfg.Reranker,
		candidateMul:            normalizeCandidateMul(cfg.CandidateMul),
		adaptiveRerank:          cfg.AdaptiveRerank,
		adaptiveMarginThreshold: adaptiveMarginThreshold,
		cache:                   cfg.SemanticCache,
		queryPrefix:             cfg.QueryPrefix,
		maxQueryChars:           maxQueryChars,
		maxFragments:            maxFragments,
		maxConcurrentFragments:  maxConcurrentFragments,
		maxTopK:                 maxTopK,
		maxChunksPerFile:        maxChunksPerFile,
		fusion:                  normalizeFusionConfig(cfg.Fusion),
		fragmentAdmission:       cfg.FragmentAdmission,
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
