package pkb

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LocalFileLister walks one or more local directories for markdown files.
type LocalFileLister struct {
	roots    []string
	patterns []string
}

func NewLocalFileLister(roots []string, ignorePatterns []string) *LocalFileLister {
	return &LocalFileLister{roots: roots, patterns: ignorePatterns}
}

func (l *LocalFileLister) ListMarkdownFiles(_ context.Context, _ string) ([]FileEntry, error) {
	var files []FileEntry
	for _, root := range l.roots {
		if err := l.walk(root, &files); err != nil {
			return nil, err
		}
	}
	return files, nil
}

func (l *LocalFileLister) walk(root string, files *[]FileEntry) error {
	return filepath.WalkDir(root, func(abs string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, abs)
		if d.IsDir() {
			if l.shouldIgnore(rel + "/") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.ToLower(filepath.Ext(abs)) == ".md" && !l.shouldIgnore(rel) {
			sha := fileContentSHA(abs)
			*files = append(*files, FileEntry{Path: rel, SHA: sha})
		}
		return nil
	})
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
