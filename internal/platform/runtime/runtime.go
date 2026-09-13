// Package runtime builds the shared infrastructure graph used by executable
// entry points. It owns wiring, not domain policy or HTTP lifecycle.
package runtime

import (
	"context"
	"fmt"
	"sync"

	"nadir/internal/adapters/docling"
	ollamaembedding "nadir/internal/adapters/ollama/embedding"
	ollamaenrichment "nadir/internal/adapters/ollama/enrichment"
	qdrantcache "nadir/internal/adapters/qdrant/cache"
	qdrantstore "nadir/internal/adapters/qdrant/documents"
	"nadir/internal/adapters/qdrant/shared"
	"nadir/internal/adapters/reranker"
	"nadir/internal/embedding"
	"nadir/internal/knowledge/chunking"
	"nadir/internal/knowledge/enrichment"
	"nadir/internal/knowledge/indexing"
	config "nadir/internal/platform/configuration"
	"nadir/internal/retrieval/cache"
	"nadir/internal/retrieval/search"

	qdrant "github.com/qdrant/go-client/qdrant"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Options controls executable-specific changes to the configured graph.
// Evaluators can bypass optional production features without rebuilding their
// own copies of the infrastructure composition.
type Options struct {
	DisableReranker      bool
	DisableSemanticCache bool
}

// Runtime is the shared retrieval and indexing graph. Function fields are
// deliberately narrow Seams: callers receive capabilities without depending
// on adapter implementations or storage lifecycle details.
type Runtime struct {
	Clients        qdrantutil.Clients
	Embedder       embedding.Embedder
	Searcher       search.Retriever
	Ingest         indexing.Ingest
	Reset          func(context.Context) error
	Stats          func(context.Context) (qdrantstore.Stats, error)
	QdrantHealth   func(context.Context) error
	EmbeddingProbe func(context.Context) (ollamaembedding.ProbeResult, error)
	RerankerProbe  func(context.Context) (reranker.ProbeResult, error)
	Close          func() error
}

// NewDependencies builds the shared Qdrant, embedding, indexing, cache, and
// retrieval graph and validates its collections before returning. The caller
// owns the returned Runtime and must call Close when its process exits.
func NewDependencies(ctx context.Context, cfg *config.Config, log *zap.Logger, opts Options) (*Runtime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("runtime config is required")
	}
	if log == nil {
		log = zap.NewNop()
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
	qdrantHealth := qdrant.NewQdrantClient(conn)
	store, err := qdrantstore.NewDependencies(qdrantstore.DependenciesConfig{
		Clients:     clients,
		Collection:  cfg.Qdrant.Collection,
		PrefetchMul: cfg.Qdrant.PrefetchMul,
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant store init: %w", err)
	}

	emb := ollamaembedding.NewDependencies(ollamaembedding.DependenciesConfig{
		Addr:           cfg.Embedder.OllamaAddr,
		Model:          cfg.Embedder.Model,
		Dimensions:     cfg.Embedder.Dimensions,
		RequestTimeout: cfg.Embedder.RequestTimeout,
	})
	if err := store.EnsureCollection(ctx, emb.Dimensions()); err != nil {
		return nil, fmt.Errorf("qdrant ensure collection: %w", err)
	}

	var semanticCache cache.SemanticCache
	if cfg.SemanticCache.Enabled && !opts.DisableSemanticCache {
		backend, cacheErr := qdrantcache.NewDependencies(qdrantcache.DependenciesConfig{
			Clients:    clients,
			Collection: cfg.SemanticCache.Collection,
		})
		if cacheErr != nil {
			log.Error("semantic cache backend init failed", zap.Error(cacheErr))
		} else if cacheErr = backend.EnsureCollection(ctx, emb.Dimensions()); cacheErr != nil {
			log.Error("semantic cache ensure collection failed", zap.Error(cacheErr))
		} else {
			candidate, candidateErr := cache.NewDependencies(cache.DependenciesConfig{
				Backend:     backend,
				Embedder:    emb,
				Threshold:   cfg.SemanticCache.Threshold,
				TTL:         cfg.SemanticCache.TTL,
				QueryPrefix: cfg.Embedder.QueryPrefix,
				Version: "v1:" + cfg.Embedder.Model + ":" + fmt.Sprint(cfg.Embedder.Dimensions) +
					":" + cfg.Embedder.QueryPrefix + ":" + cfg.Embedder.DocumentPrefix,
			})
			if candidateErr != nil {
				log.Error("semantic cache init failed", zap.Error(candidateErr))
			} else {
				semanticCache = candidate
				log.Info("semantic cache enabled",
					zap.String("collection", cfg.SemanticCache.Collection),
					zap.Float32("threshold", cfg.SemanticCache.Threshold))
			}
		}
	}

	var rerankerProbe func(context.Context) (reranker.ProbeResult, error)
	searchConfig := search.DependenciesConfig{
		Embedder:               emb,
		Store:                  store,
		CandidateMul:           cfg.Reranker.CandidateMul,
		SemanticCache:          semanticCache,
		QueryPrefix:            cfg.Embedder.QueryPrefix,
		MaxQueryChars:          cfg.Search.MaxQueryChars,
		MaxFragments:           cfg.Search.MaxFragments,
		MaxConcurrentFragments: cfg.Search.MaxConcurrentFragments,
		MaxTopK:                cfg.Search.MaxTopK,
		MaxChunksPerFile:       cfg.Search.MaxChunksPerFile,
		Log:                    log,
	}
	if cfg.Reranker.Enabled && !opts.DisableReranker {
		rankerAdapter := reranker.NewDependencies(reranker.DependenciesConfig{
			Addr:           cfg.Reranker.Addr,
			Model:          cfg.Reranker.Model,
			MaxConcurrent:  cfg.Reranker.MaxConcurrent,
			RequestTimeout: cfg.Reranker.RequestTimeout,
			Log:            log,
		})
		searchConfig.Reranker = rankerAdapter
		rerankerProbe = rankerAdapter.Probe
		log.Info("cross-encoder reranker enabled", zap.String("addr", cfg.Reranker.Addr))
	}

	searcher := search.NewDependencies(searchConfig)

	chunker := chunking.NewDependencies(chunking.DependenciesConfig{
		Provider:     cfg.Chunker.Provider,
		ChunkSize:    cfg.Chunker.ChunkSize,
		ChunkOverlap: cfg.Chunker.ChunkOverlap,
		WindowSize:   cfg.Chunker.WindowSize,
	})
	if cfg.Chunker.Provider == chunking.ProviderSentenceWindow {
		log.Info("sentence-window chunker enabled", zap.Int("window_size", cfg.Chunker.WindowSize))
	}

	var enricher enrichment.Enricher
	if cfg.Enrichment.Hype.Enabled || cfg.Enrichment.Contextual.Enabled {
		enricher = ollamaenrichment.NewDependencies(ollamaenrichment.DependenciesConfig{
			HypeAddr:        cfg.Enrichment.Hype.OllamaAddr,
			HypeModel:       cfg.Enrichment.Hype.Model,
			ContextualAddr:  cfg.Enrichment.Contextual.OllamaAddr,
			ContextualModel: cfg.Enrichment.Contextual.Model,
			RequestTimeout:  cfg.Enrichment.RequestTimeout,
		})
	}

	var converter *docling.Converter
	if cfg.Docling.Enabled {
		converter = docling.NewDependencies(docling.DependenciesConfig{
			Addr:           cfg.Docling.Addr,
			RequestTimeout: cfg.Docling.RequestTimeout,
			Log:            log,
		})
	}

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
		HypeEnabled:       cfg.Enrichment.Hype.Enabled,
		HypeQuestions:     cfg.Enrichment.Hype.QuestionsPerChunk,
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
		Log:              log,
	})

	var closeOnce sync.Once
	var closeErr error
	closeRuntime := func() error {
		closeOnce.Do(func() { closeErr = conn.Close() })
		return closeErr
	}
	closed = true
	return &Runtime{
		Clients:  clients,
		Embedder: emb,
		Searcher: searcher,
		Ingest:   ingestService,
		Reset:    ingestService.DeleteAll,
		Stats:    store.Stats,
		QdrantHealth: func(ctx context.Context) error {
			_, err := qdrantHealth.HealthCheck(ctx, &qdrant.HealthCheckRequest{})
			return err
		},
		EmbeddingProbe: emb.Probe,
		RerankerProbe:  rerankerProbe,
		Close:          closeRuntime,
	}, nil
}
