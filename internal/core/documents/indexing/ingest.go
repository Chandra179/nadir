package indexing

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nadir/internal/core/documents/chunking"
	"nadir/internal/core/observability"

	"github.com/cenkalti/backoff/v4"
	"log/slog"
)

// Run performs one indexing pass: deduplication and document intake happen
// before each source file enters the ordered chunk → enrich → embed → replace
// pipeline on a bounded worker set.
func (d *dependencies) Run(ctx context.Context, files []UploadFile, options RunOptions) (Result, error) {
	ctx, operation := observability.Start(ctx, d.telemetry, d.log, "indexing")
	var operationErr error
	defer func() {
		outcome := "success"
		if operationErr != nil {
			outcome = "error"
		}
		operation.End(outcome, operationErr, slog.Int("files", len(files)))
	}()
	result, err := d.run(ctx, files, options)
	operationErr = err
	result.OperationID = operation.ID()
	return result, err
}

func (d *dependencies) run(ctx context.Context, files []UploadFile, options RunOptions) (Result, error) {
	if d.gate != nil {
		releaseGate, err := d.gate(ctx)
		if err != nil {
			return Result{}, fmt.Errorf("indexing gate: %w", err)
		}
		defer releaseGate()
	}

	// A single process must not let two passes plan against the same SHA
	// snapshot and then commit in completion order. Distributed workers need a
	// lease/fencing token later; this lock provides the explicit single-node
	// ownership guarantee for now.
	d.runMu.Lock()
	defer d.runMu.Unlock()
	if d.coordinator != nil {
		if err := d.coordinator.BeginIngest(ctx); err != nil {
			return Result{}, fmt.Errorf("indexing lifecycle gate: %w", err)
		}
		defer d.coordinator.EndIngest()
	}

	started := time.Now()
	storedSHAs, err := d.store.GetAllFileSHAs(ctx)
	if err != nil {
		observability.StageContext(ctx, d.log, "ingest", "error", started, err, slog.Int("files", len(files)))
		return Result{}, err
	}

	var processed, skipped, failed atomic.Int64
	sem := make(chan struct{}, d.workers)
	var wg sync.WaitGroup
	seenNames := make(map[string]struct{}, len(files))

	for _, f := range files {
		if _, seen := seenNames[f.Name]; seen {
			skipped.Add(1)
			continue
		}
		seenNames[f.Name] = struct{}{}
		if d.maxFileBytes > 0 && int64(len(f.Data)) > d.maxFileBytes {
			failed.Add(1)
			d.log.Warn("skipping oversized file",
				slog.String("path", f.Name), slog.Int64("bytes", int64(len(f.Data))),
				slog.Int64("max_bytes", d.maxFileBytes))
			continue
		}
		sha := contentSHA(f.Data)
		if storedSHAs[f.Name] == sha {
			skipped.Add(1)
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(f UploadFile, sha string) {
			defer wg.Done()
			defer func() { <-sem }()

			if !isSupportedSource(f.Name) {
				err := fmt.Errorf("only .md and .pdf files can be ingested: %s", f.Name)
				failed.Add(1)
				d.log.Warn("skipping unsupported source file", slog.String("path", f.Name), slog.Any("error", err))
				return
			}
			if strings.EqualFold(filepath.Ext(f.Name), ".pdf") {
				if d.converter == nil {
					err := fmt.Errorf("PDF intake is disabled; configure docling to ingest %s", f.Name)
					failed.Add(1)
					d.log.Warn("skipping PDF without document converter", slog.String("path", f.Name), slog.Any("error", err))
					return
				}
				markdown, err := d.converter.Convert(ctx, f.Name, f.Data)
				if err != nil {
					failed.Add(1)
					d.log.Error("document intake failed", slog.String("path", f.Name), slog.Any("error", err))
					return
				}
				if d.maxFileBytes > 0 && int64(len(markdown)) > d.maxFileBytes {
					err := fmt.Errorf("converted document exceeds max file size of %d bytes", d.maxFileBytes)
					failed.Add(1)
					d.log.Warn("skipping oversized converted document", slog.String("path", f.Name), slog.Any("error", err))
					return
				}
				f.Data = markdown
			}
			if err := d.indexFile(ctx, f.Name, string(f.Data), sha); err != nil {
				d.log.Error("ingest failed", slog.String("path", f.Name), slog.Any("error", err))
				failed.Add(1)
				return
			}
			processed.Add(1)
		}(f, sha)
	}
	wg.Wait()
	removed := 0
	var reconcileErr error
	if options.MirrorSources && failed.Load() == 0 {
		removed, reconcileErr = d.reconcileSources(ctx, files, options.SourceRoots, storedSHAs)
	} else if options.MirrorSources && failed.Load() > 0 {
		d.log.Warn("source reconciliation skipped because indexing failed",
			slog.Int64("failed", failed.Load()))
	}
	d.clearSemanticCache(ctx, processed.Load() > 0 || removed > 0)

	result := Result{
		Processed: int(processed.Load()),
		Skipped:   int(skipped.Load()),
		Failed:    int(failed.Load()),
		Removed:   removed,
	}
	observability.StageContext(ctx, d.log, "ingest", "success", started, nil,
		slog.Int("files", len(files)), slog.Int("processed", result.Processed),
		slog.Int("skipped", result.Skipped), slog.Int("failed", result.Failed),
		slog.Int("removed", result.Removed))
	if reconcileErr != nil {
		return result, reconcileErr
	}
	return result, nil
}

// reconcileSources removes active Documents that belong to the configured
// source roots but were absent from a successful source sweep. It is run only
// after all discovered files have been indexed successfully, so a transient
// read, conversion, or embedding failure cannot turn into data loss.
func (d *dependencies) reconcileSources(ctx context.Context, files []UploadFile, roots []string, stored map[string]string) (int, error) {
	normalizedRoots, err := normalizeSourceRoots(roots)
	if err != nil {
		return 0, err
	}
	if len(normalizedRoots) == 0 {
		return 0, fmt.Errorf("source mirroring requires at least one source root")
	}

	desired := make(map[string]struct{}, len(files))
	for _, file := range files {
		normalized, err := normalizeSourcePath(file.Name)
		if err != nil {
			return 0, err
		}
		desired[normalized] = struct{}{}
	}

	paths := make([]string, 0, len(stored))
	for filePath := range stored {
		paths = append(paths, filePath)
	}
	sort.Strings(paths)

	removed := 0
	for _, filePath := range paths {
		normalized, err := normalizeSourcePath(filePath)
		if err != nil {
			return removed, err
		}
		if !withinAnySourceRoot(normalized, normalizedRoots) {
			continue
		}
		if _, present := desired[normalized]; present {
			continue
		}
		if err := d.store.DeleteDocument(ctx, filePath); err != nil {
			return removed, fmt.Errorf("remove missing source %q: %w", filePath, err)
		}
		removed++
	}
	return removed, nil
}

func isSupportedSource(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".md" || ext == ".pdf"
}

func contentSHA(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

// indexPlan contains the planned replacement points for one Document. Dense
// embeddings cover "<document prefix><contextual text>"; the BM25 leg
// indexes the same contextual text without the prefix.
type indexPlan struct {
	filePath  string
	sourceSHA string
	chunks    []IndexedChunk
}

// indexFile is the indexing pass seam for one Document. Planning contains
// every expensive transformation; committing is the only operation allowed
// to replace the source's stored points.
func (d *dependencies) indexFile(ctx context.Context, filePath, text, sourceSHA string) error {
	plan, err := d.planFile(ctx, filePath, text, sourceSHA)
	if err != nil {
		return err
	}
	return d.commitPlan(ctx, plan)
}

func (d *dependencies) planFile(ctx context.Context, filePath, text, sourceSHA string) (indexPlan, error) {
	started := time.Now()
	chunks, err := d.chunker.Chunk(text, filePath)
	if err != nil {
		observability.Stage(d.log, "ingest_plan", "error", started, err, slog.String("path", filePath))
		return indexPlan{}, fmt.Errorf("chunk %s: %w", filePath, err)
	}
	if d.maxChunks > 0 && len(chunks) > d.maxChunks {
		err := fmt.Errorf("file %s produces %d chunks, exceeding limit %d", filePath, len(chunks), d.maxChunks)
		observability.Stage(d.log, "ingest_plan", "error", started, err, slog.String("path", filePath), slog.Int("chunks", len(chunks)))
		return indexPlan{}, err
	}

	// Contextual text per chunk: static path/header prefix, optionally
	// enriched with an LLM-written intro.
	ctxTexts := make([]string, len(chunks))
	for i, c := range chunks {
		ctxTexts[i] = d.contextualText(ctx, text, c, d.chunker.ContextualText(c))
	}

	embedInputs := make([]string, len(chunks))
	for i := range ctxTexts {
		embedInputs[i] = d.documentPrefix + ctxTexts[i]
		// Clamp the dense input to the embedder's usable context so Ollama
		// never truncates it silently; the BM25 leg keeps the full text.
		if d.maxInputChars > 0 {
			embedInputs[i] = clampRunes(embedInputs[i], d.maxInputChars)
		}
	}
	vecs, err := d.embedWithRetry(ctx, embedInputs)
	if err != nil {
		observability.Stage(d.log, "ingest_plan", "error", started, err, slog.String("path", filePath), slog.Int("chunks", len(chunks)))
		return indexPlan{}, fmt.Errorf("embed %s: %w", filePath, err)
	}

	indexed := make([]IndexedChunk, 0, len(chunks))
	for i, c := range chunks {
		indexed = append(indexed, IndexedChunk{
			Text:       c.Text,
			WindowText: c.WindowText,
			FilePath:   c.FilePath,
			Header:     c.Header,
			LineStart:  c.LineStart,
			ChunkIndex: c.ChunkIndex,
			Vector:     vecs[i],
			SparseText: ctxTexts[i],
			SourceSHA:  sourceSHA,
		})
	}

	observability.Stage(d.log, "ingest_plan", "success", started, nil,
		slog.String("path", filePath), slog.Int("chunks", len(indexed)))
	return indexPlan{filePath: filePath, sourceSHA: sourceSHA, chunks: indexed}, nil
}

func (d *dependencies) commitPlan(ctx context.Context, plan indexPlan) error {
	started := time.Now()
	// Store replacement is versioned: it stages the new points, makes that
	// version visible, then deactivates and cleans up older versions. Retrying
	// the whole operation is safe because the version identity is deterministic.
	op := func() error {
		if err := d.store.ReplaceDocument(ctx, plan.filePath, plan.sourceSHA, plan.chunks); err != nil {
			return fmt.Errorf("replace %s: %w", plan.filePath, err)
		}
		return nil
	}
	if err := backoff.RetryNotify(op, d.newBackoff(), nil); err != nil {
		observability.Stage(d.log, "ingest_commit", "error", started, err,
			slog.String("path", plan.filePath), slog.Int("points", len(plan.chunks)))
		return err
	}
	observability.Stage(d.log, "ingest_commit", "success", started, nil,
		slog.String("path", plan.filePath), slog.Int("points", len(plan.chunks)))
	return nil
}

// clearSemanticCache drops cached answers whose source content may have
// changed. Only runs when something was actually ingested — an all-skipped
// sweep has nothing stale to invalidate, and clearing unconditionally would
// wipe a warm cache for no reason. Best-effort: failures are logged.
func (d *dependencies) clearSemanticCache(ctx context.Context, changed bool) {
	if d.cache == nil || !changed {
		return
	}
	if err := d.cache.Clear(ctx); err != nil {
		d.log.Warn("failed to clear semantic cache after ingest", slog.Any("error", err))
	}
}

// clampRunes truncates s to at most n runes.
func clampRunes(s string, n int) string {
	if n <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// contextualText fronts the chunk's contextual text with an LLM-written
// situational intro when contextual retrieval is enabled. Best-effort:
// generation failures (or empty intros) fall back to the plain text.
func (d *dependencies) contextualText(ctx context.Context, docText string, c chunking.Chunk, base string) string {
	if !d.contextual || d.enrich == nil {
		return base
	}
	intro, err := d.enrich.ContextualIntro(ctx, documentExcerpt(docText, docExcerptChars), c.Text)
	if err != nil {
		d.log.Warn("contextual enrichment failed; indexing chunk without it",
			slog.String("path", c.FilePath), slog.Int("chunk", c.ChunkIndex), slog.Any("error", err))
		return base
	}
	if intro == "" {
		return base
	}
	return intro + "\n" + base
}

// embedWithRetry embeds all inputs, preferring one batch call, with the
// standard ingest backoff applied.
func (d *dependencies) embedWithRetry(ctx context.Context, inputs []string) ([][]float32, error) {
	started := time.Now()
	finish := func(err error) {
		outcome := "success"
		if err != nil {
			outcome = "error"
		}
		observability.Stage(d.log, "document_embedding", outcome, started, err, slog.Int("inputs", len(inputs)))
	}
	if be, ok := d.embedder.(batchEmbedder); ok {
		vecs := make([][]float32, 0, len(inputs))
		for start := 0; start < len(inputs); start += d.embedBatchSize {
			end := min(start+d.embedBatchSize, len(inputs))
			batch := inputs[start:end]
			var batchVecs [][]float32
			op := func() error {
				var e error
				batchVecs, e = be.EmbedBatch(ctx, batch)
				return e
			}
			if err := backoff.RetryNotify(op, d.newBackoff(), nil); err != nil {
				finish(err)
				return nil, err
			}
			if len(batchVecs) != len(batch) {
				err := fmt.Errorf("batch %d returned %d vectors for %d inputs", start/d.embedBatchSize, len(batchVecs), len(batch))
				finish(err)
				return nil, err
			}
			vecs = append(vecs, batchVecs...)
		}
		finish(nil)
		return vecs, nil
	}
	vecs := make([][]float32, len(inputs))
	for i, t := range inputs {
		op := func() error {
			var e error
			vecs[i], e = d.embedder.Embed(ctx, t)
			return e
		}
		if err := backoff.RetryNotify(op, d.newBackoff(), nil); err != nil {
			finish(err)
			return nil, fmt.Errorf("input %d: %w", i, err)
		}
	}
	finish(nil)
	return vecs, nil
}

const docExcerptChars = 2500

// documentExcerpt truncates a document to roughly max characters at a word
// boundary, for fitting into the enrichment LLM's prompt.
func documentExcerpt(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	cut := string(runes[:max])
	if i := strings.LastIndexAny(cut, " \n"); i > max/2 {
		cut = cut[:i]
	}
	return cut
}

func (d *dependencies) newBackoff() backoff.BackOff {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = d.cfg.InitialInterval
	b.MaxInterval = d.cfg.MaxInterval
	b.Multiplier = d.cfg.Multiplier
	return backoff.WithMaxRetries(b, d.cfg.MaxAttempts)
}
