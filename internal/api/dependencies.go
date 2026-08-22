package api

import (
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
		sourceRoots: cfg.SourceRoots,
		topK:        cfg.TopK,
		log:         cfg.Log,
	}
}
