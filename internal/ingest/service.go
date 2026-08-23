package ingest

import "context"

type Result struct {
	Processed int
	Skipped   int
	Failed    int
}

// UploadFile is one file submitted to POST /ingest as multipart form data.
// Name is used both as the store's FilePath key (for dedup and citations)
// and to derive the chunker's file-extension check.
type UploadFile struct {
	Name string
	Data []byte
}

// Ingest runs an ingest pass over a batch of uploaded files: chunk, embed,
// and upsert each one that's new or changed since the last run (by content
// SHA-256).
type Ingest interface {
	Run(ctx context.Context, files []UploadFile) (Result, error)
	// Status returns a snapshot of the current or most recently finished
	// run, for dashboard polling.
	Status() RunStatus
	// History returns completed runs, most recent first.
	History() []RunSummary
}
