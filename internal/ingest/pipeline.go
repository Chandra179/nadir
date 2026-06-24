package ingest

import (
	"context"
	"fmt"

	"nadir/config"
	"nadir/internal/chunker"
	"nadir/internal/embedder"
	"nadir/internal/store"

	"github.com/cenkalti/backoff/v4"
)

type PipelineConfig = config.RetryConfig

type Pipeline struct {
	chunker  chunker.Chunker
	embedder embedder.Embedder
	store    store.Store
	cfg      PipelineConfig
}

func NewPipeline(chunker chunker.Chunker, embedder embedder.Embedder, s store.Store, cfg PipelineConfig) *Pipeline {
	return &Pipeline{chunker: chunker, embedder: embedder, store: s, cfg: cfg}
}

func (p *Pipeline) Ingest(ctx context.Context, filePath, text, sourceSHA string) error {
	chunks, err := p.chunker.Chunk(text, filePath)
	if err != nil {
		return fmt.Errorf("chunk %s: %w", filePath, err)
	}

	embedTexts := make([]string, len(chunks))
	for i, c := range chunks {
		embedTexts[i] = chunker.ContextualText(c)
	}

	var vecs [][]float32
	if be, ok := p.embedder.(embedder.BatchEmbedder); ok {
		op := func() error {
			var e error
			vecs, e = be.EmbedBatch(ctx, embedTexts)
			return e
		}
		if err := backoff.RetryNotify(op, p.newBackoff(), nil); err != nil {
			return fmt.Errorf("batch embed %s: %w", filePath, err)
		}
	} else {
		vecs = make([][]float32, len(chunks))
		for i, t := range embedTexts {
			op := func() error {
				var e error
				vecs[i], e = p.embedder.Embed(ctx, t)
				return e
			}
			if err := backoff.RetryNotify(op, p.newBackoff(), nil); err != nil {
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

	if err := p.store.Upsert(ctx, scored); err != nil {
		return fmt.Errorf("upsert %s: %w", filePath, err)
	}
	return nil
}

func (p *Pipeline) newBackoff() backoff.BackOff {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = p.cfg.InitialInterval
	b.MaxInterval = p.cfg.MaxInterval
	b.Multiplier = p.cfg.Multiplier
	return backoff.WithMaxRetries(b, p.cfg.MaxAttempts)
}
