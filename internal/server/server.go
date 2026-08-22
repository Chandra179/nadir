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
	"nadir/internal/logger"
	"nadir/internal/middleware"
	"nadir/internal/reranker"
	"nadir/internal/search"
	"nadir/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

const (
	defaultRerankerCandidate = 3
)

func Server(ctx context.Context, cfg *config.Config) {
	log, err := logger.New(cfg.Middleware.Logger.Level)
	if err != nil {
		return
	}
	defer log.Sync()

	deps := middleware.NewDependencies(middleware.DependenciesConfig{
		Logger:   log,
		Registry: prometheus.NewRegistry(),
	})

	s, err := store.NewDependencies(store.DependenciesConfig{
		Addr:        cfg.Qdrant.Addr,
		Collection:  cfg.Qdrant.Collection,
		PrefetchMul: cfg.Qdrant.PrefetchMul,
		Log:         log,
	})
	if err != nil {
		log.Error("qdrant init failed", zap.Error(err))
		return
	}

	e := embedder.NewDependencies(embedder.DependenciesConfig{
		Addr:       cfg.Embedder.OllamaAddr,
		Model:      cfg.Embedder.Model,
		Dimensions: cfg.Embedder.Dimensions,
	})

	if err := s.EnsureCollection(context.Background(), e.Dimensions()); err != nil {
		log.Error("qdrant ensure collection failed", zap.Error(err))
		return
	}

	chunkr := chunker.NewDependencies(chunker.DependenciesConfig{
		Provider:     cfg.Chunker.Provider,
		ChunkSize:    cfg.Chunker.ChunkSize,
		ChunkOverlap: cfg.Chunker.ChunkOverlap,
		WindowSize:   cfg.Chunker.WindowSize,
	})
	if cfg.Chunker.Provider == chunker.ProviderSentenceWindow {
		log.Info("sentence-window chunker enabled", zap.Int("window_size", cfg.Chunker.WindowSize))
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
		log.Info("cross-encoder reranker enabled", zap.String("addr", cfg.Reranker.Addr))
	}

	var gen generator.Generator
	if cfg.Generator.Enabled {
		ollamaAddr := cfg.Generator.OllamaAddr
		if ollamaAddr == "" {
			ollamaAddr = cfg.Embedder.OllamaAddr
		}
		gen = generator.NewDependencies(generator.DependenciesConfig{
			Addr:             ollamaAddr,
			Model:            cfg.Generator.Model,
			MaxContextTokens: cfg.Generator.MaxContextTokens,
		})
		log.Info("LLM generator enabled",
			zap.String("model", cfg.Generator.Model),
			zap.Int("max_context_tokens", cfg.Generator.MaxContextTokens),
		)
	}

	apiDeps := api.NewDependencies(api.DependenciesConfig{
		Search:      searchService,
		Ingest:      ingestDeps,
		Store:       s,
		Generator:   gen,
		SourceRoots: cfg.Source.Paths,
		TopK:        cfg.Qdrant.TopK,
		Log:         log,
	})

	var semanticCache cache.Cache

	if cfg.SemanticCache.Enabled {
		var err error
		semanticCache, err = cache.NewDependencies(cache.DependenciesConfig{
			Addr:       cfg.Qdrant.Addr,
			Collection: cfg.SemanticCache.Collection,
			Embedder:   e,
			Threshold:  cfg.SemanticCache.Threshold,
			TTL:        cfg.SemanticCache.TTL,
		})
		if err != nil {
			log.Error("semantic cache init failed", zap.Error(err))
		} else {
			if err := semanticCache.EnsureCollection(context.Background()); err != nil {
				log.Error("semantic cache ensure collection failed", zap.Error(err))
			} else {
				searchService.WithSemanticCache(semanticCache)
				ingestDeps.WithCache(semanticCache)
				log.Info("semantic cache enabled",
					zap.String("collection", cfg.SemanticCache.Collection),
					zap.Float32("threshold", cfg.SemanticCache.Threshold),
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
		log.Info("http server shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error("http server shutdown error", zap.Error(err))
		}
	}()

	log.Info("http server starting", zap.String("addr", srv.Addr))
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Error("http server error", zap.Error(err))
	}
}
