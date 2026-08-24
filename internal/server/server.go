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
	"nadir/internal/enrichment"
	"nadir/internal/generator"
	"nadir/internal/history"
	"nadir/internal/ingest"
	"nadir/internal/logger"
	"nadir/internal/middleware"
	"nadir/internal/reranker"
	"nadir/internal/search"
	"nadir/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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

	// Shared gRPC connection to Qdrant: store and the semantic cache both
	// talk to the same address, so they reuse one connection instead of
	// each dialing their own.
	qdrantConn, err := grpc.NewClient(cfg.Qdrant.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Error("qdrant dial failed", zap.Error(err))
		return
	}
	defer qdrantConn.Close()

	s, err := store.NewDependencies(store.DependenciesConfig{
		Conn:        qdrantConn,
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
		Chunker:  chunkr,
		Embedder: e,
		Store:    s,
		Retry: ingest.RetryConfig{
			MaxAttempts:     cfg.Ingest.MaxAttempts,
			InitialInterval: cfg.Ingest.InitialInterval,
			MaxInterval:     cfg.Ingest.MaxInterval,
			Multiplier:      cfg.Ingest.Multiplier,
		},
		DocumentPrefix: cfg.Embedder.DocumentPrefix,
		Log:            log,
	})

	// Index-time LLM enrichment (HyPE questions, contextual intros): both
	// are one-time costs per chunk at ingest, zero query-time latency.
	// Enabling either after a collection was already ingested requires a
	// reindex to take effect.
	if cfg.Enrichment.Hype.Enabled || cfg.Enrichment.Contextual.Enabled {
		enrichAddr := cfg.Enrichment.Hype.OllamaAddr
		enrichModel := cfg.Enrichment.Hype.Model
		if cfg.Enrichment.Contextual.Enabled {
			if enrichAddr == "" {
				enrichAddr = cfg.Enrichment.Contextual.OllamaAddr
			}
			if enrichModel == "" {
				enrichModel = cfg.Enrichment.Contextual.Model
			}
		}
		if enrichAddr == "" {
			enrichAddr = cfg.Generator.OllamaAddr
		}
		if enrichAddr == "" {
			enrichAddr = cfg.Embedder.OllamaAddr
		}
		if enrichModel == "" {
			enrichModel = cfg.Generator.Model
		}
		ingestDeps.WithEnrichment(
			enrichment.NewDependencies(enrichment.DependenciesConfig{Addr: enrichAddr, Model: enrichModel}),
			cfg.Enrichment.Hype.QuestionsPerChunk,
			cfg.Enrichment.Contextual.Enabled,
		)
		log.Info("index-time LLM enrichment enabled",
			zap.Bool("hype", cfg.Enrichment.Hype.Enabled),
			zap.Int("questions_per_chunk", cfg.Enrichment.Hype.QuestionsPerChunk),
			zap.Bool("contextual", cfg.Enrichment.Contextual.Enabled),
			zap.String("model", enrichModel),
			zap.String("addr", enrichAddr))
	}

	searchService := search.NewDependencies(search.DependenciesConfig{Embedder: e, Store: s, QueryPrefix: cfg.Embedder.QueryPrefix, Log: log})

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

	var semanticCache cache.Cache

	if cfg.SemanticCache.Enabled {
		var err error
		semanticCache, err = cache.NewDependencies(cache.DependenciesConfig{
			Conn:       qdrantConn,
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

	var hist history.History
	if cfg.History.Enabled {
		h, err := history.NewDependencies(history.DependenciesConfig{
			Conn:       qdrantConn,
			Collection: cfg.History.Collection,
			Embedder:   e,
			Log:        log,
		})
		if err != nil {
			log.Error("history init failed", zap.Error(err))
		} else if err := h.EnsureCollection(context.Background()); err != nil {
			log.Error("history ensure collection failed", zap.Error(err))
		} else {
			hist = h
			log.Info("chat history persistence enabled", zap.String("collection", cfg.History.Collection))
		}
	}

	apiDeps := api.NewDependencies(api.DependenciesConfig{
		Search:      searchService,
		Ingest:      ingestDeps,
		Store:       s,
		Generator:   gen,
		Cache:       semanticCache,
		History:     hist,
		SourceRoots: cfg.Source.Paths,
		TopK:        cfg.Qdrant.TopK,
		Config:      cfg,
		Log:         log,
	})

	engine := gin.New()
	engine.Use(gin.Recovery(), middleware.RequestID, middleware.Timeout(cfg.Middleware.Timeout), deps.RequestLog(), deps.Metrics())
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
