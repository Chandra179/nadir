// Package assembly builds the retrieval and indexing graph from injected
// provider adapters. It owns domain wiring and process-local gates, not
// transport or storage choices, so it links into any runtime (server, browser).
package assembly

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	"nadir/internal/providers/reranker"
)

// Store is the document persistence capability: the union of the indexing and
// retrieval seams plus collection lifecycle. Qdrant and the browser store both
// implement it.
type Store interface {
	EnsureCollection(ctx context.Context, dimensions int) error
	GetAllFileSHAs(ctx context.Context) (map[string]string, error)
	ReplaceDocument(ctx context.Context, filePath, sourceSHA string, chunks []indexing.IndexedChunk) error
	DeleteDocument(ctx context.Context, filePath string) error
	DeleteAll(ctx context.Context) error
	HybridSearch(ctx context.Context, vector []float32, query string, topK int, filter *search.Filter) (search.HybridSearchResult, error)
	KeywordSearch(ctx context.Context, keyword string, topK int, filter *search.Filter) ([]search.SearchCandidate, error)
}

// CacheBackend persists semantic-cache entries.
type CacheBackend interface {
	EnsureCollection(ctx context.Context, dimensions int) error
	Find(ctx context.Context, vector []float32, threshold float32) (cache.Entry, bool, error)
	Put(ctx context.Context, query string, vector []float32, entry cache.Entry) error
	Clear(ctx context.Context) error
}

// Reranker reorders retrieval candidates.
type Reranker interface {
	Rerank(ctx context.Context, query string, candidates []search.SearchCandidate) ([]search.SearchCandidate, error)
}

// Converter turns an uploaded non-text document into indexable text.
type Converter interface {
	Convert(ctx context.Context, name string, data []byte) ([]byte, error)
}

// Providers are the adapters Assemble wires together. Store and Embedder are
// required; the rest are optional and must be left as nil interfaces (never
// typed nils) when absent.
type Providers struct {
	// Telemetry is the recorder adapters report to. Assemble creates one when nil.
	Telemetry *observability.Recorder

	Store     Store
	Embedder  embedding.Embedder
	Cache     CacheBackend
	Reranker  Reranker
	Enricher  enrichment.Enricher
	Converter Converter

	StoreHealth    func(context.Context) error
	EmbeddingProbe func(context.Context) (ollamaembedding.ProbeResult, error)
	RerankerProbe  func(context.Context) (reranker.ProbeResult, error)

	// Close releases adapter resources such as connections. It runs after
	// background cache writes drain.
	Close func() error
}

// Runtime is the shared retrieval and indexing graph. Function fields are
// deliberately narrow Seams: callers receive capabilities without depending
// on adapter implementations or storage lifecycle details.
type Runtime struct {
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

// Assemble builds the indexing, cache, and retrieval graph over the supplied
// providers and validates their collections before returning. The caller owns
// the returned Runtime and must call Close when its process exits. When
// Assemble fails it does not close prov; the caller still owns those resources.
func Assemble(ctx context.Context, cfg *config.Config, log *slog.Logger, prov Providers) (*Runtime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("runtime config is required")
	}
	if prov.Store == nil || prov.Embedder == nil {
		return nil, fmt.Errorf("store and embedder providers are required")
	}
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	telemetry := prov.Telemetry
	if telemetry == nil {
		telemetry = observability.NewRecorder()
	}
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

	emb := prov.Embedder
	store := prov.Store
	if err := store.EnsureCollection(ctx, emb.Dimensions()); err != nil {
		return nil, fmt.Errorf("ensure collection: %w", err)
	}

	var semanticCache cache.SemanticCache
	if cfg.SemanticCache.Enabled && prov.Cache != nil {
		if cacheErr := prov.Cache.EnsureCollection(ctx, emb.Dimensions()); cacheErr != nil {
			log.Error("semantic cache ensure collection failed", slog.Any("error", cacheErr))
		} else {
			candidate, candidateErr := cache.NewDependencies(cache.DependenciesConfig{
				Backend:   prov.Cache,
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
	closed := false
	defer func() {
		if !closed {
			_ = cacheWrites.Close(context.Background())
		}
	}()
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
	if prov.Reranker != nil {
		searchConfig.Reranker = prov.Reranker
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

	lifecycle := indexing.NewDocumentLifecycle()
	var clearCache func(context.Context) error
	if semanticCache != nil {
		clearCache = semanticCache.Clear
	}
	ingestConfig := indexing.DependenciesConfig{
		Chunker:           chunker,
		Embedder:          emb,
		Store:             store,
		Coordinator:       lifecycle,
		CacheInvalidator:  semanticCache,
		Enricher:          prov.Enricher,
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
	}
	if prov.Converter != nil {
		ingestConfig.DocumentConverter = prov.Converter
	}
	ingestService := indexing.NewDependencies(ingestConfig)

	var closeOnce sync.Once
	var closeErr error
	closeRuntime := func() error {
		closeOnce.Do(func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			closeErr = cacheWrites.Close(shutdownCtx)
			if prov.Close != nil {
				closeErr = errors.Join(closeErr, prov.Close())
			}
		})
		return closeErr
	}
	closed = true
	return &Runtime{
		Gates:            operationGates,
		Telemetry:        telemetry,
		Embedder:         emb,
		Searcher:         searcher,
		Ingest:           ingestService,
		Reset:            ingestService.DeleteAll,
		DocumentVersions: store.GetAllFileSHAs,
		QdrantHealth:     prov.StoreHealth,
		EmbeddingProbe:   prov.EmbeddingProbe,
		RerankerProbe:    prov.RerankerProbe,
		Close:            closeRuntime,
	}, nil
}
