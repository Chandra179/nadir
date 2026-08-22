package api

import (
	"nadir/internal/generator"
	"nadir/internal/ingest"
	"nadir/internal/search"

	"go.uber.org/zap"
)

// DependenciesConfig groups everything needed to construct the API
// dependencies.
type DependenciesConfig struct {
	Search    search.Search
	Ingest    ingest.Ingest
	Generator generator.Generator
	TopK      int
	Log       *zap.Logger
}

type dependencies struct {
	search    search.Search
	ingest    ingest.Ingest
	generator generator.Generator
	topK      int
	log       *zap.Logger
}

func NewDependencies(cfg DependenciesConfig) *dependencies {
	return &dependencies{
		search:    cfg.Search,
		ingest:    cfg.Ingest,
		generator: cfg.Generator,
		topK:      cfg.TopK,
		log:       cfg.Log,
	}
}
