package ingest

import "context"

// Ingest runs an ingest pass over a batch of uploaded files: chunk, embed,
// and upsert each one that's new or changed since the last run (by content
// SHA-256).
type Ingest interface {
	Run(ctx context.Context, files []UploadFile) (Result, error)
}

// LifecycleCoordinator serializes a complete Indexing pass against
// destructive Document operations in the composition root. The current
// Adapter is process-local; distributed workers will need a shared lease and
// fencing token instead.
type LifecycleCoordinator interface {
	BeginIngest()
	EndIngest()
}

// DocumentConverter is the document-intake seam. It converts one supported
// source file into Markdown while preserving the caller's source identity.
type DocumentConverter interface {
	Convert(ctx context.Context, name string, data []byte) ([]byte, error)
}
