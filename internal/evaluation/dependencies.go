// Package evaluation measures Retrieval quality against a committed golden
// query set.
package evaluation

import (
	"nadir/internal/retrieval/search"

	"go.uber.org/zap"
)

// DependenciesConfig groups the Retrieval seam exercised by the harness.
type DependenciesConfig struct {
	Searcher search.Retriever
	Log      *zap.Logger
}

// Harness evaluates a GoldenSet through one configured Retrieval Adapter.
type Harness struct {
	searcher search.Retriever
	log      *zap.Logger
}

// NewDependencies constructs a Retrieval evaluation Harness.
func NewDependencies(cfg DependenciesConfig) *Harness {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &Harness{searcher: cfg.Searcher, log: log}
}
