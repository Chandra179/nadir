package api

import (
	"nadir/config"
	"nadir/internal/cache"
	"nadir/internal/generator"
	"nadir/internal/history"
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
	// History is optional: when nil, chat sessions/turns are not persisted
	// and the sidebar's chat list is simply empty.
	History history.History
	// SourceRoots are the configured source.paths roots, used to render
	// the dashboard's "recent roots" quick-fill chips.
	SourceRoots []string
	TopK        int
	// Config is the fully-loaded, env-overridden config used to boot this
	// server — surfaced read-only via the settings panel so users can see
	// what's actually running without shelling in to read config.yaml.
	Config *config.Config
	Log    *zap.Logger
}

type dependencies struct {
	search      search.Search
	ingest      ingest.Ingest
	store       store.Store
	generator   generator.Generator
	cache       cache.Cache
	history     history.History
	sourceRoots []string
	topK        int
	cfg         *config.Config
	log         *zap.Logger
}

func NewDependencies(cfg DependenciesConfig) *dependencies {
	return &dependencies{
		search:      cfg.Search,
		ingest:      cfg.Ingest,
		store:       cfg.Store,
		generator:   cfg.Generator,
		cache:       cfg.Cache,
		history:     cfg.History,
		sourceRoots: cfg.SourceRoots,
		topK:        cfg.TopK,
		cfg:         cfg.Config,
		log:         cfg.Log,
	}
}
