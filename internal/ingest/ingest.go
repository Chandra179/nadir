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
	"nadir/internal/store"

	"github.com/cenkalti/backoff/v4"
	"go.uber.org/zap"
)

// Enricher performs index-time LLM enrichment. Both methods are best-effort:
// failures degrade to indexing the chunk without enrichment.
type Enricher interface {
	// HypotheticalQuestions generates up to n short user questions that the
	// given chunk answers (HyPE).
	HypotheticalQuestions(ctx context.Context, header, text string, n int) ([]string, error)
	// ContextualIntro writes a short situational summary situating chunkText
	// within documentExcerpt (Anthropic-style contextual retrieval).
	ContextualIntro(ctx context.Context, documentExcerpt, chunkText string) (string, error)
}

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
		t := d.chunker.ContextualText(c)
		if d.contextual && d.enrich != nil {
			intro, err := d.enrich.ContextualIntro(ctx, documentExcerpt(text, docExcerptChars), c.Text)
			if err != nil {
				d.log.Warn("contextual enrichment failed; indexing chunk without it",
					zap.String("path", filePath), zap.Int("chunk", c.ChunkIndex), zap.Error(err))
			} else if intro != "" {
				t = intro + "\n" + t
			}
		}
		ctxTexts[i] = t
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

	if d.enrich != nil && d.hypeQuestions > 0 {
		siblings, err := d.hypeSiblings(ctx, filePath, chunks, sourceSHA)
		if err != nil {
			d.log.Warn("HyPE question embedding failed; indexing without hype points",
				zap.String("path", filePath), zap.Error(err))
		} else {
			scored = append(scored, siblings...)
		}
	}

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

type hypeSibling struct {
	parentIdx int
	question  string
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
