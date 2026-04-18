package pkb

import "context"

// Fetcher retrieves raw file content.
type Fetcher interface {
	FetchFile(ctx context.Context, path, sha string) (string, error)
}

// FileEntry represents a markdown file.
type FileEntry struct {
	Path string
	SHA  string
}

// FileLister lists markdown files for ingestion.
type FileLister interface {
	ListMarkdownFiles(ctx context.Context, sha string) ([]FileEntry, error)
}
