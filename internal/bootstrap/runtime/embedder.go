package runtime

import (
	"context"
	"fmt"
	"strings"

	"nadir/internal/bootstrap/configuration"
	"nadir/internal/core/embedding"
	"nadir/internal/core/observability"
	cloudflareembedding "nadir/internal/providers/cloudflare/embedding"
	ollamaembedding "nadir/internal/providers/ollama/embedding"
)

// providerEmbedder is what the runtime hands to indexing, retrieval, cache and
// history: single and batch embedding over one vector size. Consumers narrow
// it to the capability they use.
type providerEmbedder interface {
	embedding.Embedder
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}

// newEmbedder builds the embedder and its readiness probe for the configured
// provider.
//
// Workers AI is a hosted API: probing it on every readiness poll would spend
// quota and fail on transient network errors, so its probe reports the
// configured model and vector size only. The adapter validates the dimension of
// every response, so a wrong embedder.dimensions fails on the first embed.
func newEmbedder(cfg *config.Config, telemetry *observability.Recorder) (providerEmbedder, func(context.Context) (embedding.ProbeResult, error), error) {
	if strings.EqualFold(cfg.Embedder.Provider, "cloudflare") {
		emb, err := cloudflareembedding.NewDependencies(cloudflareembedding.DependenciesConfig{
			AccountID:      cfg.Cloudflare.AccountID,
			APIToken:       cfg.Cloudflare.APIToken,
			Model:          cfg.Embedder.Model,
			Dimensions:     cfg.Embedder.Dimensions,
			RequestTimeout: cfg.Embedder.RequestTimeout,
			Telemetry:      telemetry,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("cloudflare embedder: %w", err)
		}
		probe := func(context.Context) (embedding.ProbeResult, error) {
			return embedding.ProbeResult{ConfiguredModel: cfg.Embedder.Model, Dimensions: cfg.Embedder.Dimensions}, nil
		}
		return emb, probe, nil
	}
	emb := ollamaembedding.NewDependencies(ollamaembedding.DependenciesConfig{
		Addr:           cfg.Embedder.OllamaAddr,
		Model:          cfg.Embedder.Model,
		Dimensions:     cfg.Embedder.Dimensions,
		NumGPU:         cfg.Embedder.NumGPU,
		RequestTimeout: cfg.Embedder.RequestTimeout,
		KeepAlive:      cfg.Inference.Ollama.KeepAlive.String(),
		Telemetry:      telemetry,
	})
	return emb, emb.Probe, nil
}
