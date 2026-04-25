package pkb

import (
	"context"
	"fmt"
	"strings"
	"time"

	"nadir/config"
	"nadir/pkg/otel"

	"github.com/cenkalti/backoff/v4"
)

// PipelineConfig is an alias for config.RetryConfig.
type PipelineConfig = config.RetryConfig

// Pipeline orchestrates chunk → embed → store for a single file.
type Pipeline struct {
	chunker        Chunker
	embedder       Embedder
	sparseEmbedder SparseEmbedder // optional; when set, sparse vectors are stored alongside dense
	store          Store
	cfg            PipelineConfig
	metrics        *otel.Metrics // nil = no-op
}

func NewPipeline(chunker Chunker, embedder Embedder, store Store, cfg PipelineConfig) *Pipeline {
	return &Pipeline{chunker: chunker, embedder: embedder, store: store, cfg: cfg}
}

// WithSparseEmbedder enables sparse vector indexing at ingest time.
func (p *Pipeline) WithSparseEmbedder(se SparseEmbedder) *Pipeline {
	p.sparseEmbedder = se
	return p
}

// WithMetrics attaches an otel.Metrics recorder.
func (p *Pipeline) WithMetrics(m *otel.Metrics) *Pipeline {
	p.metrics = m
	return p
}

// Ingest chunks, embeds, and upserts a single markdown file.
func (p *Pipeline) Ingest(ctx context.Context, filePath, text, sourceSHA string) error {
	chunks, err := p.chunker.Chunk(text, filePath)
	if err != nil {
		return fmt.Errorf("chunk %s: %w", filePath, err)
	}

	embedTexts := make([]string, len(chunks))
	for i, c := range chunks {
		embedTexts[i] = contextualText(c)
	}

	// Batch embed all chunks in one round-trip when possible.
	var vecs [][]float32
	if be, ok := p.embedder.(BatchEmbedder); ok {
		embedStart := time.Now()
		op := func() error {
			var e error
			vecs, e = be.EmbedBatch(ctx, embedTexts)
			return e
		}
		if err := backoff.RetryNotify(op, p.newBackoff(), nil); err != nil {
			return fmt.Errorf("batch embed %s: %w", filePath, err)
		}
		p.metrics.RecordEmbed(ctx, time.Since(embedStart), len(embedTexts))
	} else {
		vecs = make([][]float32, len(chunks))
		for i, t := range embedTexts {
			embedStart := time.Now()
			op := func() error {
				var e error
				vecs[i], e = p.embedder.Embed(ctx, t)
				return e
			}
			if err := backoff.RetryNotify(op, p.newBackoff(), nil); err != nil {
				return fmt.Errorf("embed chunk in %s: %w", filePath, err)
			}
			p.metrics.RecordEmbed(ctx, time.Since(embedStart), 1)
		}
	}

	scored := make([]ScoredChunk, 0, len(chunks))
	for i, c := range chunks {
		var (
			sparseIndices []uint32
			sparseValues  []float32
			sparseErr     error
		)
		if p.sparseEmbedder != nil {
			sparseIndices, sparseValues, sparseErr = p.sparseEmbedder.EmbedSparse(ctx, embedTexts[i], "passage")
			if sparseErr != nil {
				return fmt.Errorf("sparse embed chunk in %s: %w", filePath, sparseErr)
			}
		}
		scored = append(scored, ScoredChunk{
			DocumentChunk: c,
			Vector:        vecs[i],
			SparseIndices: sparseIndices,
			SparseValues:  sparseValues,
			SourceSHA:     sourceSHA,
		})
	}

	if err := p.store.Upsert(ctx, scored); err != nil {
		return fmt.Errorf("upsert %s: %w", filePath, err)
	}
	return nil
}

// Delete removes all chunks for a file.
func (p *Pipeline) Delete(ctx context.Context, filePath string) error {
	return p.store.DeleteByFile(ctx, filePath)
}

// contextualText prepends file + heading context to chunk text before embedding.
// Improves retrieval by anchoring semantics to document structure (Anthropic 2024).
func contextualText(c DocumentChunk) string {
	var sb strings.Builder
	sb.WriteString(c.FilePath)
	if c.Header != "" {
		sb.WriteString(" > ")
		sb.WriteString(c.Header)
	}
	sb.WriteString("\n")
	sb.WriteString(c.Text)
	return sb.String()
}

func (p *Pipeline) newBackoff() backoff.BackOff {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = p.cfg.InitialInterval
	b.MaxInterval = p.cfg.MaxInterval
	b.Multiplier = p.cfg.Multiplier
	return backoff.WithMaxRetries(b, p.cfg.MaxAttempts)
}
