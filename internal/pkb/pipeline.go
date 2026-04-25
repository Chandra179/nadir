package pkb

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"nadir/config"

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
}

func NewPipeline(chunker Chunker, embedder Embedder, store Store, cfg PipelineConfig) *Pipeline {
	return &Pipeline{chunker: chunker, embedder: embedder, store: store, cfg: cfg}
}

// WithSparseEmbedder enables sparse vector indexing at ingest time.
func (p *Pipeline) WithSparseEmbedder(se SparseEmbedder) *Pipeline {
	p.sparseEmbedder = se
	return p
}

// Ingest chunks, embeds, and upserts a single markdown file.
func (p *Pipeline) Ingest(ctx context.Context, filePath, text, sourceSHA string) error {
	chunks, err := p.chunker.Chunk(text, filePath)
	if err != nil {
		return fmt.Errorf("chunk %s: %w", filePath, err)
	}

	scored := make([]ScoredChunk, 0, len(chunks))
	for _, c := range chunks {
		embedText := contextualText(c)

		var (
			vec           []float32
			sparseIndices []uint32
			sparseValues  []float32
			embedErr      error
			sparseErr     error
		)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			op := func() error {
				var e error
				vec, e = p.embedder.Embed(ctx, embedText)
				return e
			}
			embedErr = backoff.RetryNotify(op, p.newBackoff(), nil)
		}()

		if p.sparseEmbedder != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sparseIndices, sparseValues, sparseErr = p.sparseEmbedder.EmbedSparse(ctx, embedText, "passage")
			}()
		}
		wg.Wait()

		if embedErr != nil {
			return fmt.Errorf("embed chunk in %s: %w", filePath, embedErr)
		}
		if sparseErr != nil {
			return fmt.Errorf("sparse embed chunk in %s: %w", filePath, sparseErr)
		}

		scored = append(scored, ScoredChunk{
			DocumentChunk: c,
			Vector:        vec,
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
