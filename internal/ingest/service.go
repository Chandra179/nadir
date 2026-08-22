package ingest

import "context"

type Result struct {
	Processed int
	Skipped   int
	Failed    int
}

type Ingest interface {
	Run(ctx context.Context) (Result, error)
}
