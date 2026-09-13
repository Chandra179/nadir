package indexing

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// DiscoverFiles reads supported source files from configured source paths. Ignore
// patterns match either the path relative to a configured directory or the
// file's base name. The returned order is stable so repeated source sweeps
// are deterministic and easier to observe.
func DiscoverFiles(paths []string, ignorePatterns []string, maxFileBytes int64) ([]UploadFile, error) {
	var files []UploadFile
	for _, rawRoot := range paths {
		root, err := expandHome(rawRoot)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(root)
		if err != nil {
			return nil, fmt.Errorf("source %q: %w", rawRoot, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("source %q is not a directory", rawRoot)
		}

		var rootFiles []string
		err = filepath.WalkDir(root, func(filePath string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() != "." && strings.HasPrefix(entry.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !isSupportedSource(entry.Name()) {
				return nil
			}
			rel, err := filepath.Rel(root, filePath)
			if err != nil {
				return err
			}
			if matchesIgnore(rel, entry.Name(), ignorePatterns) {
				return nil
			}
			rootFiles = append(rootFiles, filePath)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk source %q: %w", rawRoot, err)
		}
		sort.Strings(rootFiles)

		for _, filePath := range rootFiles {
			data, err := readBoundedFile(filePath, maxFileBytes)
			if err != nil {
				return nil, fmt.Errorf("read source %q: %w", filePath, err)
			}
			files = append(files, UploadFile{Name: filePath, Data: data})
		}
	}
	return files, nil
}

func readBoundedFile(filePath string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if maxBytes <= 0 {
		return io.ReadAll(f)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("exceeds max file size of %d bytes", maxBytes)
	}
	return data, nil
}

func expandHome(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand source %q: %w", p, err)
		}
		if p == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(p, "~/")), nil
	}
	return filepath.Clean(p), nil
}

func normalizeSourceRoots(roots []string) ([]string, error) {
	normalized := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		path, err := normalizeSourcePath(root)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		normalized = append(normalized, path)
	}
	return normalized, nil
}

func normalizeSourcePath(raw string) (string, error) {
	path, err := expandHome(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", fmt.Errorf("source path must not be empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve source path %q: %w", raw, err)
	}
	return filepath.Clean(absolute), nil
}

func withinAnySourceRoot(filePath string, roots []string) bool {
	for _, root := range roots {
		relative, err := filepath.Rel(root, filePath)
		if err != nil {
			continue
		}
		if relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
			return true
		}
	}
	return false
}

func matchesIgnore(rel, base string, patterns []string) bool {
	rel = filepath.ToSlash(rel)
	for _, raw := range patterns {
		pattern := filepath.ToSlash(strings.TrimSpace(raw))
		if pattern == "" {
			continue
		}
		if ok, _ := path.Match(pattern, rel); ok {
			return true
		}
		if ok, _ := path.Match(pattern, base); ok {
			return true
		}
	}
	return false
}
