package pkb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// LocalFetcher reads file content from the local filesystem (git submodule).
type LocalFetcher struct {
	root string
}

func NewLocalFetcher(root string) *LocalFetcher {
	return &LocalFetcher{root: root}
}

// FetchFile reads the file at root/path. The sha parameter is unused for local files.
// If path is absolute (set by LocalFileLister for multi-root ingestion), root is not prepended.
func (f *LocalFetcher) FetchFile(_ context.Context, path, _ string) (string, error) {
	full := path
	if !filepath.IsAbs(path) {
		full = filepath.Join(f.root, path)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("local fetch %s: %w", path, err)
	}
	return string(data), nil
}
