package httpserver

import (
	"context"
	"net/http"
	"time"

	"nadir/config"
	"nadir/internal/cache"
	"nadir/internal/chunker"
	"nadir/internal/embedder"
	"nadir/internal/generator"
	"nadir/internal/ingest"
	"nadir/internal/middleware"
	"nadir/internal/reranker"
	"nadir/internal/search"
	"nadir/internal/store"

	"github.com/Chandra179/gosdk/logger"
)

const (
	providerSentenceWindow = "sentence-window"

	defaultWindowSize        = 3
	defaultRerankerCandidate = 3
	defaultCacheCollection   = "search_cache"
	defaultCacheThreshold    = 0.90

	routeSearch = "POST /search"
	routeIngest = "POST /ingest"
	routeHealth = "GET /healthz"
)

func Server(ctx context.Context, cfg *config.Config) {
	log := logger.NewLogger(cfg.Middleware.Logger.Level)
	deps := middleware.NewDependencies(log)

	globalChain := func(h http.Handler) http.Handler {
		return middleware.Chain(h,
			deps.Recovery(),
			middleware.RequestID,
			middleware.Timeout(middleware.TimeoutConfig{Duration: cfg.Middleware.Timeout}),
		)
	}

	s, err := store.NewQdrantStore(cfg.Qdrant.Addr, cfg.Qdrant.Collection, cfg.Qdrant.PrefetchMul, log)
	if err != nil {
		log.Error(context.Background(), "qdrant init failed", logger.Field{Key: "error", Value: err.Error()})
		return
	}

	e := embedder.NewOllamaEmbedder(cfg.Embedder.OllamaAddr, cfg.Embedder.Model, cfg.Embedder.Dimensions)

	if err := s.EnsureCollection(context.Background(), e.Dimensions()); err != nil {
		log.Error(context.Background(), "qdrant ensure collection failed", logger.Field{Key: "error", Value: err.Error()})
		return
	}

	var chunkr chunker.Chunker
	if cfg.Chunker.Provider == providerSentenceWindow {
		windowSize := cfg.Chunker.WindowSize
		if windowSize <= 0 {
			windowSize = defaultWindowSize
		}
		chunkr = chunker.NewSentenceWindowChunker(windowSize)
		log.Info(context.Background(), "sentence-window chunker enabled", logger.Field{Key: "window_size", Value: windowSize})
	} else {
		chunkr = chunker.NewRecursiveChunker(cfg.Chunker.ChunkSize, cfg.Chunker.ChunkOverlap)
	}

	pipeline := ingest.NewPipeline(chunkr, e, s, ingest.PipelineConfig{
		MaxAttempts:     cfg.Retry.MaxAttempts,
		InitialInterval: cfg.Retry.InitialInterval,
		MaxInterval:     cfg.Retry.MaxInterval,
		Multiplier:      cfg.Retry.Multiplier,
	})

	searchService := search.NewService(e, s, log)

	if cfg.Reranker.Enabled {
		mul := cfg.Reranker.CandidateMul
		if mul < 1 {
			mul = defaultRerankerCandidate
		}
		searchService.WithReranker(reranker.NewHTTPReranker(cfg.Reranker.Addr, cfg.Reranker.MaxConcurrent), mul)
		log.Info(context.Background(), "cross-encoder reranker enabled", logger.Field{Key: "addr", Value: cfg.Reranker.Addr})
	}

	searchHandler := NewSearchHandler(searchService, cfg.Qdrant.TopK)

	var semanticCache *cache.SemanticCache

	if cfg.SemanticCache.Enabled {
		col := cfg.SemanticCache.Collection
		if col == "" {
			col = defaultCacheCollection
		}
		threshold := cfg.SemanticCache.Threshold
		if threshold == 0 {
			threshold = defaultCacheThreshold
		}
		var err error
		semanticCache, err = cache.NewSemanticCache(cfg.Qdrant.Addr, col, e, threshold, cfg.SemanticCache.TTL)
		if err != nil {
			log.Error(context.Background(), "semantic cache init failed", logger.Field{Key: "error", Value: err.Error()})
		} else {
			if err := semanticCache.EnsureCollection(context.Background()); err != nil {
				log.Error(context.Background(), "semantic cache ensure collection failed", logger.Field{Key: "error", Value: err.Error()})
			} else {
				searchHandler.WithSemanticCache(semanticCache)
				log.Info(context.Background(), "semantic cache enabled",
					logger.Field{Key: "collection", Value: col},
					logger.Field{Key: "threshold", Value: threshold},
				)
			}
		}
	}

	if cfg.Generator.Enabled {
		ollamaAddr := cfg.Generator.OllamaAddr
		if ollamaAddr == "" {
			ollamaAddr = cfg.Embedder.OllamaAddr
		}
		gen := generator.NewOllamaGenerator(ollamaAddr, cfg.Generator.Model, cfg.Generator.MaxContextTokens)
		searchHandler.WithGenerator(gen)
		log.Info(context.Background(), "LLM generator enabled",
			logger.Field{Key: "model", Value: cfg.Generator.Model},
			logger.Field{Key: "max_context_tokens", Value: cfg.Generator.MaxContextTokens},
		)
	}

	ingestHandler := NewIngestHandler(cfg.Source.Paths, cfg.Ingest.IgnorePatterns, pipeline, s, semanticCache, log)

	mux := http.NewServeMux()
	mux.Handle(routeSearch, globalChain(searchHandler))
	mux.Handle(routeIngest, globalChain(ingestHandler))
	mux.HandleFunc(routeHealth, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      mux,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,
	}

	go func() {
		<-ctx.Done()
		log.Info(context.Background(), "http server shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error(context.Background(), "http server shutdown error", logger.Field{Key: "error", Value: err.Error()})
		}
	}()

	log.Info(context.Background(), "http server starting", logger.Field{Key: "addr", Value: srv.Addr})
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Error(context.Background(), "http server error", logger.Field{Key: "error", Value: err.Error()})
	}
}
