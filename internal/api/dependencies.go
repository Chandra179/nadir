package api

import (
	"nadir/internal/cache"
	"nadir/internal/generator"
	"nadir/internal/ingest"
	"nadir/internal/search"
	"nadir/internal/store"

	"go.uber.org/zap"
)

// DependenciesConfig groups everything needed to construct the API
// dependencies.
type DependenciesConfig struct {
	Search    search.Search
	Ingest    ingest.Ingest
	Store     store.Store
	Generator generator.Generator
	// Cache is optional: when set, a full data reset also clears the
	// semantic cache so it can't keep serving results for deleted content.
	Cache cache.Cache
	// SourceRoots are the configured source.paths roots, used to render
	// the dashboard's "recent roots" quick-fill chips.
	SourceRoots []string
	TopK        int
	Log         *zap.Logger
}

type dependencies struct {
	search      search.Search
	ingest      ingest.Ingest
	store       store.Store
	generator   generator.Generator
	cache       cache.Cache
	sourceRoots []string
	topK        int
	log         *zap.Logger
}

func NewDependencies(cfg DependenciesConfig) *dependencies {
	return &dependencies{
		search:      cfg.Search,
		ingest:      cfg.Ingest,
		store:       cfg.Store,
		generator:   cfg.Generator,
		cache:       cfg.Cache,
		sourceRoots: cfg.SourceRoots,
		topK:        cfg.TopK,
		log:         cfg.Log,
	}
}
