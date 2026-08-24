package eval

import "go.uber.org/zap"

// DependenciesConfig groups everything needed to construct the eval
// harness.
type DependenciesConfig struct {
	// Searcher is the retrieval entry point under test (typically the
	// search service).
	Searcher Searcher
	Log      *zap.Logger
}

// Harness runs golden-set queries through a searcher, judges the results,
// and aggregates metrics into a report.
type Harness struct {
	searcher Searcher
	log      *zap.Logger
}

func NewDependencies(cfg DependenciesConfig) *Harness {
	return &Harness{searcher: cfg.Searcher, log: cfg.Log}
}
