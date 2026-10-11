// Package runtime builds the shared infrastructure graph used by executable
// entry points. It owns wiring, not domain policy or HTTP lifecycle.
package runtime

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"nadir/internal/bootstrap/assembly"
	config "nadir/internal/bootstrap/configuration"
	"nadir/internal/bootstrap/gates"
	"nadir/internal/core/observability"
	ollamaembedding "nadir/internal/providers/ollama/embedding"
	ollamaenrichment "nadir/internal/providers/ollama/enrichment"
	qdrantcache "nadir/internal/providers/qdrant/cache"
	qdrantstore "nadir/internal/providers/qdrant/documents"
	"nadir/internal/providers/qdrant/shared"
	"nadir/internal/providers/reranker"

	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Runtime is the shared retrieval and indexing graph over the Qdrant and
// Ollama adapters. It adds the Qdrant client bundle that server-only
// components (chat history) reuse.
type Runtime struct {
	*assembly.Runtime
	Clients qdrantutil.Clients
}

// NewDependencies builds the Qdrant and Ollama adapters, then assembles the
// shared retrieval and indexing graph and validates its collections before
// returning. The caller owns the returned Runtime and must call Close when its
// process exits.
func NewDependencies(ctx context.Context, cfg *config.Config, log *slog.Logger) (*Runtime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("runtime config is required")
	}
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	conn, err := grpc.NewClient(cfg.Qdrant.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("qdrant dial: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = conn.Close()
		}
	}()

	clients := qdrantutil.NewClients(conn)
	telemetry := observability.NewRecorder()
	qdrantHealth := qdrant.NewQdrantClient(conn)
	store, err := qdrantstore.NewDependencies(qdrantstore.DependenciesConfig{
		Clients:         clients,
		Collection:      cfg.Qdrant.Collection,
		PrefetchMul:     cfg.Qdrant.PrefetchMul,
		AdaptiveSignals: cfg.Reranker.AdaptiveEnabled || cfg.Search.Fusion.Enabled,
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant store init: %w", err)
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

	prov := assembly.Providers{
		Telemetry: telemetry,
		Store:     store,
		Embedder:  emb,
		StoreHealth: func(ctx context.Context) error {
			_, err := qdrantHealth.HealthCheck(ctx, &qdrant.HealthCheckRequest{})
			return err
		},
		EmbeddingProbe: emb.Probe,
		Close:          conn.Close,
	}

	if cfg.SemanticCache.Enabled {
		backend, cacheErr := qdrantcache.NewDependencies(qdrantcache.DependenciesConfig{
			Clients:    clients,
			Collection: cfg.SemanticCache.Collection,
		})
		if cacheErr != nil {
			log.Error("semantic cache backend init failed", slog.Any("error", cacheErr))
		} else {
			prov.Cache = backend
		}
	}

	if cfg.Reranker.Enabled {
		rankerAdapter := reranker.NewDependencies(reranker.DependenciesConfig{
			Addr:           cfg.Reranker.Addr,
			Model:          cfg.Reranker.Model,
			RequestTimeout: cfg.Reranker.RequestTimeout,
			Gate: gates.NewGate(
				cfg.Inference.Reranker.MaxConcurrent,
				cfg.Inference.Reranker.QueueTimeout,
			),
			Log: log,
		})
		prov.Reranker = rankerAdapter
		prov.RerankerProbe = rankerAdapter.Probe
		log.Info("cross-encoder reranker enabled", slog.String("addr", cfg.Reranker.Addr))
	}

	if cfg.Enrichment.Contextual.Enabled {
		prov.Enricher = ollamaenrichment.NewDependencies(ollamaenrichment.DependenciesConfig{
			ContextualAddr:  cfg.Enrichment.Contextual.OllamaAddr,
			ContextualModel: cfg.Enrichment.Contextual.Model,
			Think:           cfg.Enrichment.Contextual.Think,
			RequestTimeout:  cfg.Enrichment.RequestTimeout,
			KeepAlive:       cfg.Inference.Ollama.KeepAlive.String(),
		})
	}

	if converter := documentIntake(cfg.Docling, log); converter != nil {
		prov.Converter = converter
	}

	core, err := assembly.Assemble(ctx, cfg, log, prov)
	if err != nil {
		return nil, err
	}
	closed = true
	return &Runtime{Runtime: core, Clients: clients}, nil
}
