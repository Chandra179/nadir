package engine

import "context"

// Fetcher retrieves raw file content.
type Fetcher interface {
	FetchFile(ctx context.Context, path, sha string) (string, error)
}

// FileEntry represents a file discovered during listing.
type FileEntry struct {
	Path string
	Root string // absolute root dir this file was found in; set by LocalFileLister
	SHA  string
}

// FileLister lists files for ingestion.
type FileLister interface {
	ListFiles(ctx context.Context, sha string) ([]FileEntry, error)
}
