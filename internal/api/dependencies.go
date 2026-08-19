package api

import (
	"nadir/internal/generator"
	"nadir/internal/ingest"
	"nadir/internal/search"

	"github.com/Chandra179/gosdk/logger"
)

// DependenciesConfig groups everything needed to construct the API
// dependencies.
type DependenciesConfig struct {
	Search    *search.Dependencies
	Ingest    *ingest.Dependencies
	Generator generator.Generator
	TopK      int
	Log       logger.Logger
}

type dependencies struct {
	search    *search.Dependencies
	ingest    *ingest.Dependencies
	generator generator.Generator
	topK      int
	log       logger.Logger
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
