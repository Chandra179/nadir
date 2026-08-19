package server

import (
	"context"
	"net/http"
	"time"

	"nadir/config"
	"nadir/internal/api"
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
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	providerSentenceWindow = "sentence-window"

	defaultWindowSize        = 3
	defaultRerankerCandidate = 3
	defaultCacheCollection   = "search_cache"
	defaultCacheThreshold    = 0.90
)

func Server(ctx context.Context, cfg *config.Config) {
	log := logger.NewLogger(cfg.Middleware.Logger.Level)

	zapLevel := zapcore.InfoLevel
	_ = zapLevel.UnmarshalText([]byte(cfg.Middleware.Logger.Level))
	zapCfg := zap.NewProductionConfig()
	zapCfg.Level = zap.NewAtomicLevelAt(zapLevel)
	zapLogger, err := zapCfg.Build()
	if err != nil {
		log.Error(context.Background(), "zap logger init failed", logger.Field{Key: "error", Value: err.Error()})
		return
	}
	defer zapLogger.Sync()

	deps := middleware.NewDependencies(middleware.DependenciesConfig{
		Logger:   zapLogger,
		Registry: prometheus.NewRegistry(),
	})

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

	ingestDeps := ingest.NewDependencies(ingest.DependenciesConfig{
		Roots:          cfg.Source.Paths,
		IgnorePatterns: cfg.Ingest.IgnorePatterns,
		Chunker:        chunkr,
		Embedder:       e,
		Store:          s,
		Retry: ingest.RetryConfig{
			MaxAttempts:     cfg.Ingest.MaxAttempts,
			InitialInterval: cfg.Ingest.InitialInterval,
			MaxInterval:     cfg.Ingest.MaxInterval,
			Multiplier:      cfg.Ingest.Multiplier,
		},
		Log: log,
	})

	searchService := search.NewDependencies(search.DependenciesConfig{Embedder: e, Store: s, Log: log})

	if cfg.Reranker.Enabled {
		mul := cfg.Reranker.CandidateMul
		if mul < 1 {
			mul = defaultRerankerCandidate
		}
		searchService.WithReranker(reranker.NewDependencies(reranker.DependenciesConfig{
			Addr:          cfg.Reranker.Addr,
			MaxConcurrent: cfg.Reranker.MaxConcurrent,
		}), mul)
		log.Info(context.Background(), "cross-encoder reranker enabled", logger.Field{Key: "addr", Value: cfg.Reranker.Addr})
	}

	var gen generator.Generator
	if cfg.Generator.Enabled {
		ollamaAddr := cfg.Generator.OllamaAddr
		if ollamaAddr == "" {
			ollamaAddr = cfg.Embedder.OllamaAddr
		}
		gen = generator.NewOllamaGenerator(ollamaAddr, cfg.Generator.Model, cfg.Generator.MaxContextTokens)
		log.Info(context.Background(), "LLM generator enabled",
			logger.Field{Key: "model", Value: cfg.Generator.Model},
			logger.Field{Key: "max_context_tokens", Value: cfg.Generator.MaxContextTokens},
		)
	}

	apiDeps := api.NewDependencies(api.DependenciesConfig{
		Search:    searchService,
		Ingest:    ingestDeps,
		Generator: gen,
		TopK:      cfg.Qdrant.TopK,
		Log:       log,
	})

	var semanticCache cache.Cache

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
				searchService.WithSemanticCache(semanticCache)
				ingestDeps.WithCache(semanticCache)
				log.Info(context.Background(), "semantic cache enabled",
					logger.Field{Key: "collection", Value: col},
					logger.Field{Key: "threshold", Value: threshold},
				)
			}
		}
	}

	engine := gin.New()
	engine.Use(gin.Recovery(), middleware.RequestID, deps.RequestLog(), deps.Metrics())
	router := api.NewRouter(engine, apiDeps)

	srv := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      router,
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
