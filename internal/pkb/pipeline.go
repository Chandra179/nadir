package pkb

import (
	"context"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v4"
)

// PipelineConfig holds retry parameters.
type PipelineConfig struct {
	MaxAttempts     uint64
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Multiplier      float64
}

// Pipeline orchestrates chunk → embed → store for a single file.
type Pipeline struct {
	chunker  Chunker
	embedder Embedder
	store    Store
	cfg      PipelineConfig
}

func NewPipeline(chunker Chunker, embedder Embedder, store Store, cfg PipelineConfig) *Pipeline {
	return &Pipeline{chunker: chunker, embedder: embedder, store: store, cfg: cfg}
}

// Ingest chunks, embeds, and upserts a single markdown file.
func (p *Pipeline) Ingest(ctx context.Context, filePath, text, sourceSHA string) error {
	chunks, err := p.chunker.Chunk(text, filePath)
	if err != nil {
		return fmt.Errorf("chunk %s: %w", filePath, err)
	}

	scored := make([]ScoredChunk, 0, len(chunks))
	for _, c := range chunks {
		var vec []float32
		op := func() error {
			var e error
			vec, e = p.embedder.Embed(ctx, c.Text)
			return e
		}
		if err := backoff.RetryNotify(op, p.newBackoff(), nil); err != nil {
			return fmt.Errorf("embed chunk in %s: %w", filePath, err)
		}
		scored = append(scored, ScoredChunk{
			DocumentChunk: c,
			Vector:        vec,
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

func (p *Pipeline) newBackoff() backoff.BackOff {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = p.cfg.InitialInterval
	b.MaxInterval = p.cfg.MaxInterval
	b.Multiplier = p.cfg.Multiplier
	return backoff.WithMaxRetries(b, p.cfg.MaxAttempts)
}
