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

	"nadir/internal/chunker"
	"nadir/internal/embedder"
	"nadir/internal/observability"
	"nadir/internal/store"

	"github.com/cenkalti/backoff/v4"
	"go.uber.org/zap"
)

// Run performs one indexing pass: deduplication and document intake happen
// before each source file enters the ordered chunk → enrich → embed → replace
// pipeline on a bounded worker set.
func (d *dependencies) Run(ctx context.Context, files []UploadFile) (Result, error) {
	return d.run(ctx, files)
}

func (d *dependencies) run(ctx context.Context, files []UploadFile) (Result, error) {
	started := time.Now()
	storedSHAs, err := d.store.GetAllFileSHAs(ctx)
	if err != nil {
		observability.Stage(d.log, "ingest", "error", started, err, zap.Int("files", len(files)))
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
				zap.String("path", f.Name), zap.Int64("bytes", int64(len(f.Data))),
				zap.Int64("max_bytes", d.maxFileBytes))
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
				d.log.Warn("skipping unsupported source file", zap.String("path", f.Name), zap.Error(err))
				return
			}
			if strings.EqualFold(filepath.Ext(f.Name), ".pdf") {
				if d.converter == nil {
					err := fmt.Errorf("PDF intake is disabled; configure docling to ingest %s", f.Name)
					failed.Add(1)
					d.log.Warn("skipping PDF without document converter", zap.String("path", f.Name), zap.Error(err))
					return
				}
				markdown, err := d.converter.Convert(ctx, f.Name, f.Data)
				if err != nil {
					failed.Add(1)
					d.log.Error("document intake failed", zap.String("path", f.Name), zap.Error(err))
					return
				}
				if d.maxFileBytes > 0 && int64(len(markdown)) > d.maxFileBytes {
					err := fmt.Errorf("converted document exceeds max file size of %d bytes", d.maxFileBytes)
					failed.Add(1)
					d.log.Warn("skipping oversized converted document", zap.String("path", f.Name), zap.Error(err))
					return
				}
				f.Data = markdown
			}
			if err := d.indexFile(ctx, f.Name, string(f.Data), sha); err != nil {
				d.log.Error("ingest failed", zap.String("path", f.Name), zap.Error(err))
				failed.Add(1)
				return
			}
			processed.Add(1)
		}(f, sha)
	}
	wg.Wait()
	d.clearSemanticCache(ctx, processed.Load() > 0)

	result := Result{
		Processed: int(processed.Load()),
		Skipped:   int(skipped.Load()),
		Failed:    int(failed.Load()),
	}
	observability.Stage(d.log, "ingest", "success", started, nil,
		zap.Int("files", len(files)), zap.Int("processed", result.Processed),
		zap.Int("skipped", result.Skipped), zap.Int("failed", result.Failed))
	return result, nil
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
// indexes the same contextual text without the prefix. With HyPE enabled,
// each chunk additionally gets sibling points from embedded hypothetical
// questions.
type indexPlan struct {
	filePath  string
	sourceSHA string
	chunks    []store.ScoredChunk
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
		observability.Stage(d.log, "ingest_plan", "error", started, err, zap.String("path", filePath))
		return indexPlan{}, fmt.Errorf("chunk %s: %w", filePath, err)
	}
	if d.maxChunks > 0 && len(chunks) > d.maxChunks {
		err := fmt.Errorf("file %s produces %d chunks, exceeding limit %d", filePath, len(chunks), d.maxChunks)
		observability.Stage(d.log, "ingest_plan", "error", started, err, zap.String("path", filePath), zap.Int("chunks", len(chunks)))
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
	}
	vecs, err := d.embedWithRetry(ctx, embedInputs)
	if err != nil {
		observability.Stage(d.log, "ingest_plan", "error", started, err, zap.String("path", filePath), zap.Int("chunks", len(chunks)))
		return indexPlan{}, fmt.Errorf("embed %s: %w", filePath, err)
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
			SparseText: ctxTexts[i],
			SourceSHA:  sourceSHA,
		})
	}

	scored = d.appendHypeSiblings(ctx, scored, filePath, chunks, sourceSHA)

	observability.Stage(d.log, "ingest_plan", "success", started, nil,
		zap.String("path", filePath), zap.Int("chunks", len(scored)))
	return indexPlan{filePath: filePath, sourceSHA: sourceSHA, chunks: scored}, nil
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
			zap.String("path", plan.filePath), zap.Int("points", len(plan.chunks)))
		return err
	}
	observability.Stage(d.log, "ingest_commit", "success", started, nil,
		zap.String("path", plan.filePath), zap.Int("points", len(plan.chunks)))
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
		d.log.Warn("failed to clear semantic cache after ingest", zap.Error(err))
	}
}

// contextualText fronts the chunk's contextual text with an LLM-written
// situational intro when contextual retrieval is enabled. Best-effort:
// generation failures (or empty intros) fall back to the plain text.
func (d *dependencies) contextualText(ctx context.Context, docText string, c chunker.Chunk, base string) string {
	if !d.contextual || d.enrich == nil {
		return base
	}
	intro, err := d.enrich.ContextualIntro(ctx, documentExcerpt(docText, docExcerptChars), c.Text)
	if err != nil {
		d.log.Warn("contextual enrichment failed; indexing chunk without it",
			zap.String("path", c.FilePath), zap.Int("chunk", c.ChunkIndex), zap.Error(err))
		return base
	}
	if intro == "" {
		return base
	}
	return intro + "\n" + base
}

type hypeSibling struct {
	parentIdx int
	question  string
}

// appendHypeSiblings extends scored with HyPE sibling points when HyPE is
// enabled. Best-effort: generation/embedding failures index the file
// without hype points.
func (d *dependencies) appendHypeSiblings(ctx context.Context, scored []store.ScoredChunk, filePath string, chunks []chunker.Chunk, sourceSHA string) []store.ScoredChunk {
	if !d.hypeEnabled || d.enrich == nil || d.hypeQuestions <= 0 {
		return scored
	}
	siblings, err := d.hypeSiblings(ctx, filePath, chunks, sourceSHA)
	if err != nil {
		d.log.Warn("HyPE question embedding failed; indexing without hype points",
			zap.String("path", filePath), zap.Error(err))
		return scored
	}
	return append(scored, siblings...)
}

// hypeSiblings generates hypothetical questions per chunk, embeds them in
// one batched call, and returns sibling ScoredChunks carrying the parent's
// identity fields (so search-side Key() dedup collapses them onto the
// parent) plus their own hype marker for unique point IDs.
func (d *dependencies) hypeSiblings(ctx context.Context, filePath string, chunks []chunker.Chunk, sourceSHA string) ([]store.ScoredChunk, error) {
	var refs []hypeSibling
	for i, c := range chunks {
		qs, err := d.enrich.HypotheticalQuestions(ctx, c.Header, c.Text, d.hypeQuestions)
		if err != nil {
			d.log.Warn("HyPE generation failed for chunk; skipping its hype points",
				zap.String("path", filePath), zap.Int("chunk", c.ChunkIndex), zap.Error(err))
			continue
		}
		for _, q := range qs {
			refs = append(refs, hypeSibling{parentIdx: i, question: q})
		}
	}
	if len(refs) == 0 {
		return nil, nil
	}

	inputs := make([]string, len(refs))
	for j, r := range refs {
		inputs[j] = d.documentPrefix + r.question
	}
	vecs, err := d.embedWithRetry(ctx, inputs)
	if err != nil {
		return nil, err
	}

	out := make([]store.ScoredChunk, 0, len(refs))
	perParent := make(map[int]int)
	for j, r := range refs {
		c := chunks[r.parentIdx]
		idx := perParent[r.parentIdx]
		perParent[r.parentIdx] = idx + 1
		out = append(out, store.ScoredChunk{
			Text:         c.Text,
			WindowText:   c.WindowText,
			FilePath:     c.FilePath,
			Header:       c.Header,
			LineStart:    c.LineStart,
			ChunkIndex:   c.ChunkIndex,
			Vector:       vecs[j],
			SourceSHA:    sourceSHA,
			HypeQuestion: r.question,
			HypeIndex:    idx,
		})
	}
	return out, nil
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
		observability.Stage(d.log, "document_embedding", outcome, started, err, zap.Int("inputs", len(inputs)))
	}
	if be, ok := d.embedder.(embedder.BatchEmbedder); ok {
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
