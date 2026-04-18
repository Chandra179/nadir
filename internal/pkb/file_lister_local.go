package pkb

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LocalFileLister walks a local directory (git submodule) for markdown files.
type LocalFileLister struct {
	root     string
	patterns []string
}

func NewLocalFileLister(root string, ignorePatterns []string) *LocalFileLister {
	return &LocalFileLister{root: root, patterns: ignorePatterns}
}

func (l *LocalFileLister) ListMarkdownFiles(_ context.Context, _ string) ([]FileEntry, error) {
	var files []FileEntry
	err := filepath.Walk(l.root, func(abs string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(l.root, abs)
		if info.IsDir() {
			if l.shouldIgnore(rel + "/") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.ToLower(filepath.Ext(abs)) == ".md" && !l.shouldIgnore(rel) {
			sha := fileContentSHA(abs)
			files = append(files, FileEntry{Path: rel, SHA: sha})
		}
		return nil
	})
	return files, err
}

func fileContentSHA(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

func (l *LocalFileLister) shouldIgnore(path string) bool {
	for _, p := range l.patterns {
		if matchPattern(p, path) {
			return true
		}
	}
	return false
}

// matchPattern supports ** as "any path prefix" in addition to filepath.Match glob syntax.
func matchPattern(pattern, path string) bool {
	if base, ok := strings.CutSuffix(pattern, "/**"); ok {
		if strings.HasPrefix(path, base+"/") {
			return true
		}
	}
	ok, _ := filepath.Match(pattern, path)
	return ok
}
