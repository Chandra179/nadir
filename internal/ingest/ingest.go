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
	"time"

	"nadir/internal/embedder"
	"nadir/internal/store"

	"github.com/cenkalti/backoff/v4"
	"go.uber.org/zap"
)

func (d *dependencies) Run(ctx context.Context, target string) (Result, error) {
	startedAt := time.Now()
	d.tr.start(target)

	result, err := d.run(ctx, target)
	d.tr.finish(target, result, err, startedAt)
	return result, err
}

func (d *dependencies) run(ctx context.Context, target string) (Result, error) {
	var files []fileInfo
	var err error
	if target == "" {
		files, err = d.listFiles(ctx)
	} else {
		files, err = d.listTarget(target)
	}
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
			d.tr.record(f.path, EventSkipped, "sha match")
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(f fileInfo) {
			defer wg.Done()
			defer func() { <-sem }()

			d.tr.record(f.path, EventRunning, "embedding")

			fetchPath := f.path
			if f.root != "" {
				fetchPath = filepath.Join(f.root, f.path)
			}
			text, err := os.ReadFile(fetchPath)
			if err != nil {
				d.log.Error("read file failed", zap.String("path", f.path), zap.Error(err))
				failed.Add(1)
				d.tr.record(f.path, EventFailed, err.Error())
				return
			}
			if err := d.ingestFile(ctx, f.path, string(text), f.sha); err != nil {
				d.log.Error("ingest failed", zap.String("path", f.path), zap.Error(err))
				failed.Add(1)
				d.tr.record(f.path, EventFailed, err.Error())
				return
			}
			processed.Add(1)
			d.tr.record(f.path, EventProcessed, "")
		}(f)
	}
	wg.Wait()

	// Only clear the semantic cache when content actually changed: a full
	// sweep where every file is unchanged (all skipped) has nothing stale
	// to invalidate, so clearing unconditionally on every run wiped a warm
	// cache for no reason.
	if d.cache != nil && processed.Load() > 0 {
		if err := d.cache.Clear(ctx); err != nil {
			d.log.Warn("failed to clear semantic cache after ingest", zap.Error(err))
		}
	}

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
		absRoot, err := filepath.Abs(root)
		if err != nil {
			absRoot = root
		}
		if err := d.walkFrom(absRoot, absRoot, &files); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// listTarget scopes a run to a single file or directory instead of the full
// configured roots. target may be absolute, or relative to one of the
// configured roots; it must resolve inside a configured root.
func (d *dependencies) listTarget(target string) ([]fileInfo, error) {
	absRoot, absTarget, err := d.resolveTarget(target)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(absTarget)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", target, err)
	}

	var files []fileInfo
	if info.IsDir() {
		if err := d.walkFrom(absRoot, absTarget, &files); err != nil {
			return nil, err
		}
		return files, nil
	}

	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", target, err)
	}
	if strings.ToLower(filepath.Ext(absTarget)) != ".md" {
		return nil, fmt.Errorf("only .md files can be ingested: %s", target)
	}
	if d.shouldIgnore(rel) {
		return nil, fmt.Errorf("path is excluded by ignore patterns: %s", target)
	}
	return []fileInfo{{path: rel, root: absRoot, sha: fileContentSHA(absTarget)}}, nil
}

// resolveTarget finds the configured root that contains target and returns
// both the root's absolute path and target's absolute path. An absolute
// target must fall under one of the roots; a relative target is tried
// against each root in order until one exists on disk.
func (d *dependencies) resolveTarget(target string) (absRoot, absTarget string, err error) {
	for _, root := range d.roots {
		aRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}

		candidate := filepath.Clean(target)
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Clean(filepath.Join(aRoot, target))
		}

		rel, err := filepath.Rel(aRoot, candidate)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}

		if _, statErr := os.Stat(candidate); statErr == nil {
			return aRoot, candidate, nil
		}
	}
	return "", "", fmt.Errorf("path not found under any configured source root: %s", target)
}

// walkFrom walks the subtree rooted at start, recording files relative to
// absRoot so IDs and stored file paths stay stable whether the run covers
// the whole root or just a subset of it.
func (d *dependencies) walkFrom(absRoot, start string, files *[]fileInfo) error {
	return filepath.WalkDir(start, func(abs string, dirEntry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(absRoot, abs)
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

	// Chunk IDs are derived from filePath:lineStart:chunkIndex, so a content
	// change that shifts section/line boundaries produces different IDs
	// than the previous version of this file. Without this delete, the old
	// chunks would never be overwritten and would linger in the collection
	// as stale, orphaned points. Delete and upsert share one retry: a
	// transient failure between them (delete succeeds, upsert doesn't)
	// would otherwise leave the file with zero indexed chunks until the
	// next sweep.
	op := func() error {
		if err := d.store.DeleteByFile(ctx, filePath); err != nil {
			return fmt.Errorf("delete stale chunks for %s: %w", filePath, err)
		}
		if err := d.store.Upsert(ctx, scored); err != nil {
			return fmt.Errorf("upsert %s: %w", filePath, err)
		}
		return nil
	}
	if err := backoff.RetryNotify(op, d.newBackoff(), nil); err != nil {
		return err
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
