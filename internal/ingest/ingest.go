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

	"nadir/internal/embedder"
	"nadir/internal/store"

	"github.com/Chandra179/gosdk/logger"
	"github.com/cenkalti/backoff/v4"
)

func (d *dependencies) Run(ctx context.Context) (Result, error) {
	if d.cache != nil {
		if err := d.cache.Clear(ctx); err != nil {
			d.log.Warn(ctx, "failed to clear semantic cache before ingest", logger.Field{Key: "error", Value: err.Error()})
		}
	}

	files, err := d.listFiles(ctx)
	if err != nil {
		return Result{}, err
	}

	storedSHAs, err := d.store.GetAllFileSHAs(ctx)
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
				d.log.Error(ctx, "read file failed", logger.Field{Key: "path", Value: f.path}, logger.Field{Key: "error", Value: err.Error()})
				failed.Add(1)
				return
			}
			if err := d.ingestFile(ctx, f.path, string(text), f.sha); err != nil {
				d.log.Error(ctx, "ingest failed", logger.Field{Key: "path", Value: f.path}, logger.Field{Key: "error", Value: err.Error()})
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

func (d *dependencies) listFiles(_ context.Context) ([]fileInfo, error) {
	var files []fileInfo
	for _, root := range d.roots {
		if err := d.walk(root, &files); err != nil {
			return nil, err
		}
	}
	return files, nil
}

func (d *dependencies) walk(root string, files *[]fileInfo) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}
	return filepath.WalkDir(root, func(abs string, dirEntry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, abs)
		if dirEntry.IsDir() {
			if d.shouldIgnore(rel + "/") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.ToLower(filepath.Ext(abs)) == ".md" && !d.shouldIgnore(rel) {
			*files = append(*files, fileInfo{
				path: rel,
				root: absRoot,
				sha:  fileContentSHA(abs),
			})
		}
		return nil
	})
}

func (d *dependencies) shouldIgnore(path string) bool {
	for _, p := range d.patterns {
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

// ingestFile chunks, embeds, and upserts a single file's content.
func (d *dependencies) ingestFile(ctx context.Context, filePath, text, sourceSHA string) error {
	chunks, err := d.chunker.Chunk(text, filePath)
	if err != nil {
		return fmt.Errorf("chunk %s: %w", filePath, err)
	}

	embedTexts := make([]string, len(chunks))
	for i, c := range chunks {
		embedTexts[i] = d.chunker.ContextualText(c)
	}

	var vecs [][]float32
	if be, ok := d.embedder.(embedder.BatchEmbedder); ok {
		op := func() error {
			var e error
			vecs, e = be.EmbedBatch(ctx, embedTexts)
			return e
		}
		if err := backoff.RetryNotify(op, d.newBackoff(), nil); err != nil {
			return fmt.Errorf("batch embed %s: %w", filePath, err)
		}
	} else {
		vecs = make([][]float32, len(chunks))
		for i, t := range embedTexts {
			op := func() error {
				var e error
				vecs[i], e = d.embedder.Embed(ctx, t)
				return e
			}
			if err := backoff.RetryNotify(op, d.newBackoff(), nil); err != nil {
				return fmt.Errorf("embed chunk in %s: %w", filePath, err)
			}
		}
	}

	scored := make([]store.ScoredChunk, 0, len(chunks))
	for i, c := range chunks {
		scored = append(scored, store.ScoredChunk{
			Text:       c.Text,
			WindowText: c.WindowText,
			FilePath:   c.FilePath,
			Header:     c.Header,
			LineStart:  c.LineStart,
			ChunkIndex: c.ChunkIndex,
			Vector:     vecs[i],
			SourceSHA:  sourceSHA,
		})
	}

	if err := d.store.Upsert(ctx, scored); err != nil {
		return fmt.Errorf("upsert %s: %w", filePath, err)
	}
	return nil
}

func (d *dependencies) newBackoff() backoff.BackOff {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = d.cfg.InitialInterval
	b.MaxInterval = d.cfg.MaxInterval
	b.Multiplier = d.cfg.Multiplier
	return backoff.WithMaxRetries(b, d.cfg.MaxAttempts)
}
