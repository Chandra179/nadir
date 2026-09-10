package ingest

import (
	"net/http"
	"strings"
	"time"

	"nadir/internal/cache"
	"nadir/internal/chunker"
	"nadir/internal/embedder"
	"nadir/internal/enrichment"
	"nadir/internal/store"

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
	Chunker           chunker.Chunker
	Embedder          embedder.Embedder
	Store             store.Store
	SemanticCache     cache.SemanticCache
	Enricher          enrichment.Enricher
	DocumentConverter DocumentConverter
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
	chunker        chunker.Chunker
	embedder       embedder.Embedder
	store          store.Store
	cache          cache.SemanticCache
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
	converter      DocumentConverter
	log            *zap.Logger
}

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
	return &dependencies{
		chunker:        cfg.Chunker,
		embedder:       cfg.Embedder,
		store:          cfg.Store,
		cache:          cfg.SemanticCache,
		enrich:         cfg.Enricher,
		hypeEnabled:    cfg.HypeEnabled,
		hypeQuestions:  cfg.HypeQuestions,
		contextual:     cfg.ContextualEnabled,
		converter:      cfg.DocumentConverter,
		cfg:            cfg.Retry,
		documentPrefix: cfg.DocumentPrefix,
		maxFileBytes:   maxFileBytes,
		embedBatchSize: embedBatchSize,
		maxChunks:      maxChunks,
		workers:        workers,
		log:            log,
	}
}

// NewDoclingConverter builds the optional document-intake Adapter. The
// indexing pass still receives Markdown and keeps the original source
// identity for citations and deterministic chunk IDs.
func NewDoclingConverter(addr string, timeout time.Duration) DocumentConverter {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &doclingConverter{
		addr:   strings.TrimRight(addr, "/"),
		client: &http.Client{Timeout: timeout},
	}
}
