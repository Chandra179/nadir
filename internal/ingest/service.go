package ingest

import "context"

type Result struct {
	Processed int
	Skipped   int
	Failed    int
}

// Ingest runs an ingest pass. target selects what to (re)index: empty runs
// the full configured source.paths sweep; a non-empty value is a single
// .md file or a directory, absolute or relative to one of the configured
// roots, and scopes the run to just that path.
type Ingest interface {
	Run(ctx context.Context, target string) (Result, error)
	// Status returns a snapshot of the current or most recently finished
	// run, for dashboard polling.
	Status() RunStatus
	// History returns completed runs, most recent first.
	History() []RunSummary
}
