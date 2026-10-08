// Package runtime builds the shared infrastructure graph used by executable
// entry points. It owns wiring, not domain policy or HTTP lifecycle.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	config "nadir/internal/bootstrap/configuration"
	"nadir/internal/bootstrap/gates"
	"nadir/internal/core/documents/chunking"
	"nadir/internal/core/documents/enrichment"
	"nadir/internal/core/documents/indexing"
	"nadir/internal/core/embedding"
	"nadir/internal/core/observability"
	"nadir/internal/core/retrieval/cache"
	"nadir/internal/core/retrieval/search"
	ollamaembedding "nadir/internal/providers/ollama/embedding"
	ollamaenrichment "nadir/internal/providers/ollama/enrichment"
	qdrantcache "nadir/internal/providers/qdrant/cache"
	qdrantstore "nadir/internal/providers/qdrant/documents"
	"nadir/internal/providers/qdrant/shared"
	"nadir/internal/providers/reranker"

	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"log/slog"
)

// Runtime is the shared retrieval and indexing graph. Function fields are
// deliberately narrow Seams: callers receive capabilities without depending
// on adapter implementations or storage lifecycle details.
type Runtime struct {
	Clients          qdrantutil.Clients
	Gates            *gates.Controller
	Telemetry        *observability.Recorder
	Embedder         embedding.Embedder
	Searcher         search.Retriever
	Ingest           indexing.Ingest
	Reset            func(context.Context) error
	DocumentVersions func(context.Context) (map[string]string, error)
	QdrantHealth     func(context.Context) error
	EmbeddingProbe   func(context.Context) (ollamaembedding.ProbeResult, error)
	RerankerProbe    func(context.Context) (reranker.ProbeResult, error)
	Close            func() error
}

func retrievalFusionConfig(cfg config.FusionConfig) search.FusionConfig {
	profiles := make(map[search.QueryType]search.FusionProfile, len(cfg.Profiles))
	for name, profile := range cfg.Profiles {
		profiles[search.QueryType(name)] = search.FusionProfile{
			DenseWeight:      profile.DenseWeight,
			BM25Weight:       profile.BM25Weight,
			ExactMatchBoost:  profile.ExactMatchBoost,
			HeaderMatchBoost: profile.HeaderMatchBoost,
			MinExactTokens:   profile.MinExactTokens,
			MinHeaderTokens:  profile.MinHeaderTokens,
		}
	}
	return search.FusionConfig{
		Enabled:          cfg.Enabled,
		RRFK:             cfg.RRFK,
		DenseWeight:      cfg.DenseWeight,
		BM25Weight:       cfg.BM25Weight,
		ExactMatchBoost:  cfg.ExactMatchBoost,
		HeaderMatchBoost: cfg.HeaderMatchBoost,
		MinExactTokens:   cfg.MinExactTokens,
		MinHeaderTokens:  cfg.MinHeaderTokens,
		Profiles:         profiles,
	}
}

// NewDependencies builds the shared Qdrant, embedding, indexing, cache, and
// retrieval graph and validates its collections before returning. The caller
// owns the returned Runtime and must call Close when its process exits.
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
	// LLM and embedding concurrency is delegated to the Ollama scheduler
	// (OLLAMA_NUM_PARALLEL) plus per-role request timeouts; process-local
	// gates stay only for single-writer indexing and destructive operations.
	operationGates := gates.New(gates.Config{
		Indexing: gates.OperationConfig{
			MaxConcurrent: cfg.Gates.Indexing.MaxConcurrent,
			QueueTimeout:  cfg.Gates.Indexing.QueueTimeout,
		},
		Destructive: gates.OperationConfig{
			MaxConcurrent: cfg.Gates.Destructive.MaxConcurrent,
			QueueTimeout:  cfg.Gates.Destructive.QueueTimeout,
		},
		Recorder: telemetry,
	})
	log.Info("inference resource profile",
		slog.String("profile", cfg.Inference.Profile),
		slog.Duration("ollama_keep_alive", cfg.Inference.Ollama.KeepAlive),
		slog.Int("reranker_max_concurrent", cfg.Inference.Reranker.MaxConcurrent),
		slog.String("reranker_device", cfg.Inference.Reranker.Device),
		slog.String("reranker_backend", cfg.Inference.Reranker.Backend),
	)
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
	if err := store.EnsureCollection(ctx, emb.Dimensions()); err != nil {
		return nil, fmt.Errorf("qdrant ensure collection: %w", err)
	}

	var semanticCache cache.SemanticCache
	if cfg.SemanticCache.Enabled {
		backend, cacheErr := qdrantcache.NewDependencies(qdrantcache.DependenciesConfig{
			Clients:    clients,
			Collection: cfg.SemanticCache.Collection,
		})
		if cacheErr != nil {
			log.Error("semantic cache backend init failed", slog.Any("error", cacheErr))
		} else if cacheErr = backend.EnsureCollection(ctx, emb.Dimensions()); cacheErr != nil {
			log.Error("semantic cache ensure collection failed", slog.Any("error", cacheErr))
		} else {
			candidate, candidateErr := cache.NewDependencies(cache.DependenciesConfig{
				Backend:   backend,
				Threshold: cfg.SemanticCache.Threshold,
				TTL:       cfg.SemanticCache.TTL,
				Telemetry: telemetry,
				Version:   cachePolicyVersion(cfg),
			})
			if candidateErr != nil {
				log.Error("semantic cache init failed", slog.Any("error", candidateErr))
			} else {
				semanticCache = candidate
				log.Info("semantic cache enabled",
					slog.String("collection", cfg.SemanticCache.Collection),
					slog.Float64("threshold", float64(cfg.SemanticCache.Threshold)))
			}
		}
	}

	cacheWrites := gates.NewBackgroundRunner(2, 10*time.Second)
	defer func() {
		if !closed {
			_ = cacheWrites.Close(context.Background())
		}
	}()
	var rerankerProbe func(context.Context) (reranker.ProbeResult, error)
	searchConfig := search.DependenciesConfig{
		Embedder:                emb,
		Store:                   store,
		CandidateMul:            cfg.Reranker.CandidateMul,
		AdaptiveRerank:          cfg.Reranker.AdaptiveEnabled,
		AdaptiveMarginThreshold: cfg.Reranker.AdaptiveMarginThreshold,
		SemanticCache:           semanticCache,
		CacheWrite:              cacheWrites.Submit,
		QueryPrefix:             cfg.Embedder.QueryPrefix,
		MaxQueryChars:           cfg.Search.MaxQueryChars,
		MaxFragments:            cfg.Search.MaxFragments,
		MaxConcurrentFragments:  cfg.Search.MaxConcurrentFragments,
		MaxTopK:                 cfg.Search.MaxTopK,
		MaxChunksPerFile:        cfg.Search.MaxChunksPerFile,
		Fusion:                  retrievalFusionConfig(cfg.Search.Fusion),
		Telemetry:               telemetry,
		Log:                     log,
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
		searchConfig.Reranker = rankerAdapter
		rerankerProbe = rankerAdapter.Probe
		log.Info("cross-encoder reranker enabled", slog.String("addr", cfg.Reranker.Addr))
	}

	searcher := search.NewDependencies(searchConfig)

	chunker := chunking.NewDependencies(chunking.DependenciesConfig{
		Provider:     cfg.Chunker.Provider,
		ChunkSize:    cfg.Chunker.ChunkSize,
		ChunkOverlap: cfg.Chunker.ChunkOverlap,
		WindowSize:   cfg.Chunker.WindowSize,
	})
	if cfg.Chunker.Provider == chunking.ProviderSentenceWindow {
		log.Info("sentence-window chunker enabled", slog.Int("window_size", cfg.Chunker.WindowSize))
	}

	var enricher enrichment.Enricher
	if cfg.Enrichment.Contextual.Enabled {
		enricher = ollamaenrichment.NewDependencies(ollamaenrichment.DependenciesConfig{
			ContextualAddr:  cfg.Enrichment.Contextual.OllamaAddr,
			ContextualModel: cfg.Enrichment.Contextual.Model,
			Think:           cfg.Enrichment.Contextual.Think,
			RequestTimeout:  cfg.Enrichment.RequestTimeout,
			KeepAlive:       cfg.Inference.Ollama.KeepAlive.String(),
		})
	}

	converter := documentIntake(cfg.Docling, log)

	lifecycle := indexing.NewDocumentLifecycle()
	var clearCache func(context.Context) error
	if semanticCache != nil {
		clearCache = semanticCache.Clear
	}
	ingestService := indexing.NewDependencies(indexing.DependenciesConfig{
		Chunker:           chunker,
		Embedder:          emb,
		Store:             store,
		Coordinator:       lifecycle,
		CacheInvalidator:  semanticCache,
		Enricher:          enricher,
		DocumentConverter: converter,
		Reset:             store.DeleteAll,
		ClearCache:        clearCache,
		Gates:             operationGates.AcquireFunc(gates.Indexing),
		DestructiveGate:   operationGates.AcquireFunc(gates.Destructive),
		Telemetry:         telemetry,
		ContextualEnabled: cfg.Enrichment.Contextual.Enabled,
		Retry: indexing.RetryConfig{
			MaxAttempts:     cfg.Ingest.MaxAttempts,
			InitialInterval: cfg.Ingest.InitialInterval,
			MaxInterval:     cfg.Ingest.MaxInterval,
			Multiplier:      cfg.Ingest.Multiplier,
		},
		Workers:          cfg.Ingest.Workers,
		MaxFileBytes:     cfg.Ingest.MaxFileBytes,
		EmbedBatchSize:   cfg.Ingest.EmbedBatchSize,
		MaxChunksPerFile: cfg.Ingest.MaxChunksPerFile,
		DocumentPrefix:   cfg.Embedder.DocumentPrefix,
		MaxInputChars:    cfg.Embedder.MaxInputChars,
		Log:              log,
	})

	var closeOnce sync.Once
	var closeErr error
	closeRuntime := func() error {
		closeOnce.Do(func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			backgroundErr := cacheWrites.Close(shutdownCtx)
			closeErr = errors.Join(backgroundErr, conn.Close())
		})
		return closeErr
	}
	closed = true
	return &Runtime{
		Clients:          clients,
		Gates:            operationGates,
		Telemetry:        telemetry,
		Embedder:         emb,
		Searcher:         searcher,
		Ingest:           ingestService,
		Reset:            ingestService.DeleteAll,
		DocumentVersions: store.GetAllFileSHAs,
		QdrantHealth: func(ctx context.Context) error {
			_, err := qdrantHealth.HealthCheck(ctx, &qdrant.HealthCheckRequest{})
			return err
		},
		EmbeddingProbe: emb.Probe,
		RerankerProbe:  rerankerProbe,
		Close:          closeRuntime,
	}, nil
}
