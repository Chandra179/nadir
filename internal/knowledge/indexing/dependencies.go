package indexing

import (
	"context"
	"sync"
	"time"

	"nadir/internal/embedding"
	"nadir/internal/knowledge/chunking"
	"nadir/internal/knowledge/enrichment"

	"go.uber.org/zap"
)

const (
	ingestWorkers      = 8
	defaultEmbedBatch  = 64
	defaultMaxFileSize = 16 << 20
	defaultMaxChunks   = 10000
)

// RetryConfig controls the backoff used for retrying embed calls during ingest.
type RetryConfig struct {
	MaxAttempts     uint64
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Multiplier      float64
}

// DependenciesConfig groups everything needed to construct the ingest
// dependencies.
type DependenciesConfig struct {
	Chunker           chunking.Chunker
	Embedder          embedding.Embedder
	Store             documentIndexer
	Coordinator       lifecycleCoordinator
	CacheInvalidator  cacheInvalidator
	Enricher          enrichment.Enricher
	DocumentConverter documentConverter
	Reset             func(context.Context) error
	ClearCache        func(context.Context) error
	HypeEnabled       bool
	HypeQuestions     int
	ContextualEnabled bool
	Retry             RetryConfig
	Workers           int
	MaxFileBytes      int64
	EmbedBatchSize    int
	MaxChunksPerFile  int
	Log               *zap.Logger
	// DocumentPrefix is prepended to every embedded text at ingest time
	// (e.g. "search_document: " for nomic-embed-text task instructions).
	DocumentPrefix string
}

// dependencies takes a batch of uploaded files, dedups them by SHA-256
// against what's already stored, and for each new/changed file runs
// chunk -> embed -> upsert.
type dependencies struct {
	chunker        chunking.Chunker
	embedder       embedding.Embedder
	store          documentIndexer
	coordinator    lifecycleCoordinator
	cache          cacheInvalidator
	cfg            RetryConfig
	documentPrefix string
	enrich         enrichment.Enricher
	hypeEnabled    bool
	hypeQuestions  int
	contextual     bool
	maxFileBytes   int64
	embedBatchSize int
	maxChunks      int
	workers        int
	runMu          sync.Mutex
	converter      documentConverter
	reset          func(context.Context) error
	clearCache     func(context.Context) error
	log            *zap.Logger
}

var _ Ingest = (*dependencies)(nil)

// NewDependencies constructs the indexing pass over caller-supplied seams.
func NewDependencies(cfg DependenciesConfig) *dependencies {
	workers := cfg.Workers
	if workers <= 0 {
		workers = ingestWorkers
	}
	maxFileBytes := cfg.MaxFileBytes
	if maxFileBytes <= 0 {
		maxFileBytes = defaultMaxFileSize
	}
	embedBatchSize := cfg.EmbedBatchSize
	if embedBatchSize <= 0 {
		embedBatchSize = defaultEmbedBatch
	}
	maxChunks := cfg.MaxChunksPerFile
	if maxChunks <= 0 {
		maxChunks = defaultMaxChunks
	}
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	coordinator := cfg.Coordinator
	return &dependencies{
		chunker:        cfg.Chunker,
		embedder:       cfg.Embedder,
		store:          cfg.Store,
		coordinator:    coordinator,
		cache:          cfg.CacheInvalidator,
		enrich:         cfg.Enricher,
		hypeEnabled:    cfg.HypeEnabled,
		hypeQuestions:  cfg.HypeQuestions,
		contextual:     cfg.ContextualEnabled,
		converter:      cfg.DocumentConverter,
		reset:          cfg.Reset,
		clearCache:     cfg.ClearCache,
		cfg:            cfg.Retry,
		documentPrefix: cfg.DocumentPrefix,
		maxFileBytes:   maxFileBytes,
		embedBatchSize: embedBatchSize,
		maxChunks:      maxChunks,
		workers:        workers,
		log:            log,
	}
}
