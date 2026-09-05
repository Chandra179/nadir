package ingest

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"nadir/internal/chunker"
	"nadir/internal/embedder"
	"nadir/internal/store"

	"github.com/cenkalti/backoff/v4"
	"go.uber.org/zap"
)

// Enricher performs index-time LLM enrichment — see interface.go.
func (d *dependencies) Run(ctx context.Context, files []UploadFile) (Result, error) {
	return d.run(ctx, files)
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
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(f UploadFile, sha string) {
			defer wg.Done()
			defer func() { <-sem }()

			if strings.ToLower(filepath.Ext(f.Name)) != ".md" {
				err := fmt.Errorf("only .md files can be ingested: %s", f.Name)
				failed.Add(1)
				d.log.Warn("skipping non-markdown upload", zap.String("path", f.Name), zap.Error(err))
				return
			}
			if err := d.ingestFile(ctx, f.Name, string(f.Data), sha); err != nil {
				d.log.Error("ingest failed", zap.String("path", f.Name), zap.Error(err))
				failed.Add(1)
				return
			}
			processed.Add(1)
		}(f, sha)
	}
	wg.Wait()
	d.clearSemanticCache(ctx, processed.Load() > 0)

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

// ingestFile chunks, enriches, embeds, and upserts a single file's content.
//
// Dense embeddings are computed over "<document prefix><contextual text>",
// where the contextual text is file path > header plus the chunk body,
// optionally fronted by an LLM-written contextual intro. The BM25 sparse
// leg indexes the same contextual text without the task prefix, keeping
// both retrieval legs aligned on identical context.
//
// When HyPE is enabled, each chunk additionally gets N sibling points whose
// dense vectors are embedded hypothetical questions; siblings carry the
// parent's payload so dedup/citation logic treats them as the same chunk.
func (d *dependencies) ingestFile(ctx context.Context, filePath, text, sourceSHA string) error {
	chunks, err := d.chunker.Chunk(text, filePath)
	if err != nil {
		return fmt.Errorf("chunk %s: %w", filePath, err)
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
		return fmt.Errorf("embed %s: %w", filePath, err)
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

	// Chunk IDs are derived from filePath:lineStart:chunkIndex, so a content
	// change that shifts section/line boundaries produces different IDs
	// than the previous version of this file. Without this delete, the old
	// chunks would never be overwritten and would linger in the collection
	// as stale, orphaned points. Delete and upsert share one retry: a
	// transient failure between them (delete succeeds, upsert doesn't)
	// would otherwise leave the file with zero indexed chunks until the
	// next sweep. DeleteByFile's file_path filter also removes any HyPE
	// sibling points from the previous version of the file.
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
	if d.enrich == nil || d.hypeQuestions <= 0 {
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
	if be, ok := d.embedder.(embedder.BatchEmbedder); ok {
		var vecs [][]float32
		op := func() error {
			var e error
			vecs, e = be.EmbedBatch(ctx, inputs)
			return e
		}
		if err := backoff.RetryNotify(op, d.newBackoff(), nil); err != nil {
			return nil, err
		}
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
			return nil, fmt.Errorf("input %d: %w", i, err)
		}
	}
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
