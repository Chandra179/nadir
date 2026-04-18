package pkb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// LocalFetcher reads file content from the local filesystem (git submodule).
type LocalFetcher struct {
	Root string
}

func NewLocalFetcher(root string) *LocalFetcher {
	return &LocalFetcher{Root: root}
}

// FetchFile reads the file at Root/path. The sha parameter is unused for local files.
func (f *LocalFetcher) FetchFile(_ context.Context, path, _ string) (string, error) {
	full := filepath.Join(f.Root, path)
	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("local fetch %s: %w", path, err)
	}
	return string(data), nil
}
