package ingest

import (
	"time"

	"nadir/internal/cache"
	"nadir/internal/chunker"
	"nadir/internal/embedder"
	"nadir/internal/store"

	"go.uber.org/zap"
)

const ingestWorkers = 8

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
	Chunker  chunker.Chunker
	Embedder embedder.Embedder
	Store    store.Store
	Retry    RetryConfig
	Log      *zap.Logger
}

// dependencies takes a batch of uploaded files, dedups them by SHA-256
// against what's already stored, and for each new/changed file runs
// chunk -> embed -> upsert.
type dependencies struct {
	chunker  chunker.Chunker
	embedder embedder.Embedder
	store    store.Store
	cache    cache.Cache
	cfg      RetryConfig
	log      *zap.Logger
	tr       tracker
}

func NewDependencies(cfg DependenciesConfig) *dependencies {
	return &dependencies{
		chunker:  cfg.Chunker,
		embedder: cfg.Embedder,
		store:    cfg.Store,
		cfg:      cfg.Retry,
		log:      cfg.Log,
	}
}

// WithCache enables clearing the semantic cache at the start of every Run,
// since a fresh ingest can make cached results stale.
func (d *dependencies) WithCache(c cache.Cache) *dependencies {
	d.cache = c
	return d
}
