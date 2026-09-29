package indexing

import (
	"context"
	"io"
	"sync"
	"time"

	"nadir/internal/core/documents/chunking"
	"nadir/internal/core/documents/enrichment"
	"nadir/internal/core/embedding"
	"nadir/internal/core/observability"

	"log/slog"
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
	Gates             func(context.Context) (func(), error)
	DestructiveGate   func(context.Context) (func(), error)
	ContextualEnabled bool
	Retry             RetryConfig
	Workers           int
	MaxFileBytes      int64
	EmbedBatchSize    int
	MaxChunksPerFile  int
	Log               *slog.Logger
	Telemetry         *observability.Recorder
	// DocumentPrefix is prepended to every embedded text at ingest time
	// (e.g. "search_document: " for nomic-embed-text task instructions).
	DocumentPrefix string
	// MaxInputChars bounds the dense embed input so an over-long chunk is
	// rune-clamped before the embedder call instead of being silently
	// truncated server-side. Zero disables the clamp (direct package tests).
	MaxInputChars int
}

// dependencies takes a batch of uploaded files, dedups them by SHA-256
// against what's already stored, and for each new/changed file runs
// chunk -> embed -> upsert.
type dependencies struct {
	chunker         chunking.Chunker
	embedder        embedding.Embedder
	store           documentIndexer
	coordinator     lifecycleCoordinator
	cache           cacheInvalidator
	cfg             RetryConfig
	documentPrefix  string
	maxInputChars   int
	enrich          enrichment.Enricher
	contextual      bool
	maxFileBytes    int64
	embedBatchSize  int
	maxChunks       int
	workers         int
	runMu           sync.Mutex
	converter       documentConverter
	reset           func(context.Context) error
	clearCache      func(context.Context) error
	gate            func(context.Context) (func(), error)
	destructiveGate func(context.Context) (func(), error)
	telemetry       *observability.Recorder
	log             *slog.Logger
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
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	coordinator := cfg.Coordinator
	return &dependencies{
		chunker:         cfg.Chunker,
		embedder:        cfg.Embedder,
		store:           cfg.Store,
		coordinator:     coordinator,
		cache:           cfg.CacheInvalidator,
		enrich:          cfg.Enricher,
		contextual:      cfg.ContextualEnabled,
		converter:       cfg.DocumentConverter,
		reset:           cfg.Reset,
		clearCache:      cfg.ClearCache,
		gate:            cfg.Gates,
		destructiveGate: cfg.DestructiveGate,
		telemetry:       cfg.Telemetry,
		cfg:             cfg.Retry,
		documentPrefix:  cfg.DocumentPrefix,
		maxInputChars:   cfg.MaxInputChars,
		maxFileBytes:    maxFileBytes,
		embedBatchSize:  embedBatchSize,
		maxChunks:       maxChunks,
		workers:         workers,
		log:             log,
	}
}
