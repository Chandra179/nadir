package indexing

import "context"

// RunOptions controls the scope of one indexing pass. Source mirroring is
// deliberately opt-in because a multipart upload must never delete documents
// that are not present in that upload request.
type RunOptions struct {
	MirrorSources bool
	SourceRoots   []string
}

// Ingest runs an ingest pass over a batch of uploaded files: chunk, embed,
// and upsert each one that's new or changed since the last run (by content
// SHA-256).
type Ingest interface {
	Run(ctx context.Context, files []UploadFile, options RunOptions) (Result, error)
}
