package ingest

import (
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
	Chunker          chunker.Chunker
	Embedder         embedder.Embedder
	Store            store.Store
	Retry            RetryConfig
	MaxFileBytes     int64
	EmbedBatchSize   int
	MaxChunksPerFile int
	Log              *zap.Logger
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
	hypeQuestions  int
	contextual     bool
	maxFileBytes   int64
	embedBatchSize int
	maxChunks      int
	log            *zap.Logger
}

func NewDependencies(cfg DependenciesConfig) *dependencies {
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
		cfg:            cfg.Retry,
		documentPrefix: cfg.DocumentPrefix,
		maxFileBytes:   maxFileBytes,
		embedBatchSize: embedBatchSize,
		maxChunks:      maxChunks,
		log:            log,
	}
}

// WithSemanticCache enables clearing the semantic cache after every Run
// that actually ingested something, since new content makes cached results
// stale.
func (d *dependencies) WithSemanticCache(c cache.SemanticCache) *dependencies {
	d.cache = c
	return d
}

// WithEnrichment wires index-time LLM enrichment: hypeQuestions > 0 enables
// HyPE question siblings, contextual enables LLM-written chunk intros.
func (d *dependencies) WithEnrichment(e enrichment.Enricher, hypeQuestions int, contextual bool) *dependencies {
	d.enrich = e
	d.hypeQuestions = hypeQuestions
	d.contextual = contextual
	return d
}
