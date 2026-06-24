package ingest

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"nadir/internal/store"

	"github.com/Chandra179/gosdk/logger"
)

const ingestWorkers = 8

type Result struct {
	Processed int
	Skipped   int
	Failed    int
}

type Service struct {
	roots    []string
	patterns []string
	processor Processor
	store     store.Store
	log       logger.Logger
}

func NewService(roots []string, ignorePatterns []string, processor Processor, s store.Store, log logger.Logger) *Service {
	return &Service{
		roots:     roots,
		patterns:  ignorePatterns,
		processor: processor,
		store:     s,
		log:       log,
	}
}

func (s *Service) Run(ctx context.Context) (Result, error) {
	files, err := s.listFiles(ctx)
	if err != nil {
		return Result{}, err
	}

	storedSHAs, err := s.store.GetAllFileSHAs(ctx)
	if err != nil {
		return Result{}, err
	}

	var processed, skipped, failed atomic.Int64
	sem := make(chan struct{}, ingestWorkers)
	var wg sync.WaitGroup

	for _, f := range files {
		if f.sha != "" && storedSHAs[f.path] == f.sha {
			skipped.Add(1)
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(f fileInfo) {
			defer wg.Done()
			defer func() { <-sem }()

			fetchPath := f.path
			if f.root != "" {
				fetchPath = filepath.Join(f.root, f.path)
			}
			text, err := os.ReadFile(fetchPath)
			if err != nil {
				s.log.Error(ctx, "read file failed", logger.Field{Key: "path", Value: f.path}, logger.Field{Key: "error", Value: err.Error()})
				failed.Add(1)
				return
			}
			if err := s.processor.Ingest(ctx, f.path, string(text), f.sha); err != nil {
				s.log.Error(ctx, "ingest failed", logger.Field{Key: "path", Value: f.path}, logger.Field{Key: "error", Value: err.Error()})
				failed.Add(1)
				return
			}
			processed.Add(1)
		}(f)
	}
	wg.Wait()

	return Result{
		Processed: int(processed.Load()),
		Skipped:   int(skipped.Load()),
		Failed:    int(failed.Load()),
	}, nil
}

type fileInfo struct {
	path string
	root string
	sha  string
}

func (s *Service) listFiles(_ context.Context) ([]fileInfo, error) {
	var files []fileInfo
	for _, root := range s.roots {
		if err := s.walk(root, &files); err != nil {
			return nil, err
		}
	}
	return files, nil
}

func (s *Service) walk(root string, files *[]fileInfo) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}
	return filepath.WalkDir(root, func(abs string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, abs)
		if d.IsDir() {
			if s.shouldIgnore(rel + "/") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.ToLower(filepath.Ext(abs)) == ".md" && !s.shouldIgnore(rel) {
			*files = append(*files, fileInfo{
				path: rel,
				root: absRoot,
				sha:  fileContentSHA(abs),
			})
		}
		return nil
	})
}

func (s *Service) shouldIgnore(path string) bool {
	for _, p := range s.patterns {
		if matchPattern(p, path) {
			return true
		}
	}
	return false
}

func fileContentSHA(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

func matchPattern(pattern, path string) bool {
	if base, ok := strings.CutSuffix(pattern, "/**"); ok {
		if strings.HasPrefix(path, base+"/") {
			return true
		}
	}
	ok, _ := filepath.Match(pattern, path)
	return ok
}
