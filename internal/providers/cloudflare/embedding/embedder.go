// Package embedding embeds text with a Cloudflare Workers AI model. It owns the
// OpenAI-compatible /ai/v1/embeddings wire format and vector-shape validation;
// prefixes, input clamping and reindexing policy belong to the callers.
package embedding

import (
	"context"
	"fmt"
	"time"
)

type embeddingsRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingsResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (e *dependencies) Dimensions() int { return e.dimensions }

func (e *dependencies) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

// EmbedBatch embeds texts in order, splitting into requests of at most
// batchSize inputs.
func (e *dependencies) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += e.batchSize {
		end := min(start+e.batchSize, len(texts))
		vecs, err := e.embed(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

func (e *dependencies) embed(ctx context.Context, texts []string) ([][]float32, error) {
	start := time.Now()
	outcome := "error"
	defer func() { e.telemetry.Record("workersai.embed.total", outcome, time.Since(start)) }()

	var resp embeddingsResponse
	if err := e.client.PostJSON(ctx, "/embeddings", embeddingsRequest{Model: e.model, Input: texts}, &resp); err != nil {
		return nil, fmt.Errorf("workers ai embed: %w", err)
	}
	if len(resp.Data) != len(texts) {
		return nil, fmt.Errorf("workers ai embed: got %d embeddings for %d inputs", len(resp.Data), len(texts))
	}
	vecs := make([][]float32, len(texts))
	for _, item := range resp.Data {
		if item.Index < 0 || item.Index >= len(texts) || vecs[item.Index] != nil {
			return nil, fmt.Errorf("workers ai embed: unexpected embedding index %d", item.Index)
		}
		if len(item.Embedding) != e.dimensions {
			return nil, fmt.Errorf("workers ai embed: model %q returned %d dimensions, want %d (embedder.dimensions)", e.model, len(item.Embedding), e.dimensions)
		}
		vecs[item.Index] = item.Embedding
	}
	outcome = "success"
	return vecs, nil
}
