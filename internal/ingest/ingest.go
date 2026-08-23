package ingest

import (
	"context"
	"crypto/sha256"
	"fmt"
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

func (d *dependencies) Run(ctx context.Context, files []UploadFile) (Result, error) {
	startedAt := time.Now()
	label := runLabel(files)
	d.tr.start(label)

	result, err := d.run(ctx, files)
	d.tr.finish(label, result, err, startedAt)
	return result, err
}

func runLabel(files []UploadFile) string {
	if len(files) == 1 {
		return files[0].Name
	}
	return fmt.Sprintf("%d files", len(files))
}

func (d *dependencies) run(ctx context.Context, files []UploadFile) (Result, error) {
	storedSHAs, err := d.store.GetAllFileSHAs(ctx)
	if err != nil {
		return Result{}, err
	}

	var processed, skipped, failed atomic.Int64
	sem := make(chan struct{}, ingestWorkers)
	var wg sync.WaitGroup

	for _, f := range files {
		sha := contentSHA(f.Data)
		if sha != "" && storedSHAs[f.Name] == sha {
			skipped.Add(1)
			d.tr.record(f.Name, EventSkipped, "sha match")
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(f UploadFile, sha string) {
			defer wg.Done()
			defer func() { <-sem }()

			d.tr.record(f.Name, EventRunning, "embedding")

			if strings.ToLower(filepath.Ext(f.Name)) != ".md" {
				err := fmt.Errorf("only .md files can be ingested: %s", f.Name)
				failed.Add(1)
				d.tr.record(f.Name, EventFailed, err.Error())
				return
			}
			if err := d.ingestFile(ctx, f.Name, string(f.Data), sha); err != nil {
				d.log.Error("ingest failed", zap.String("path", f.Name), zap.Error(err))
				failed.Add(1)
				d.tr.record(f.Name, EventFailed, err.Error())
				return
			}
			processed.Add(1)
			d.tr.record(f.Name, EventProcessed, "")
		}(f, sha)
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

func contentSHA(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
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
