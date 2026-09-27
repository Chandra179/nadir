// Package evaluation measures Retrieval quality against a committed golden
// query set.
package evaluation

import (
	"io"
	"nadir/internal/core/retrieval/search"

	"log/slog"
)

// DependenciesConfig groups the Retrieval seam exercised by the harness.
type DependenciesConfig struct {
	Searcher search.Retriever
	Log      *slog.Logger
}

// Harness evaluates a GoldenSet through one configured Retrieval Adapter.
type Harness struct {
	searcher search.Retriever
	log      *slog.Logger
}

// NewDependencies constructs a Retrieval evaluation Harness.
func NewDependencies(cfg DependenciesConfig) *Harness {
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Harness{searcher: cfg.Searcher, log: log}
}
