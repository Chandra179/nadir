package server

import (
	"context"
	"fmt"
	"net/http"

	"nadir/config"
	"nadir/internal/api"
	"nadir/internal/cache"
	"nadir/internal/chat"
	"nadir/internal/chunker"
	"nadir/internal/embedder"
	"nadir/internal/enrichment"
	"nadir/internal/generator"
	"nadir/internal/history"
	"nadir/internal/ingest"
	"nadir/internal/logger"
	"nadir/internal/middleware"
	"nadir/internal/qdrantutil"
	"nadir/internal/reranker"
	"nadir/internal/rewriter"
	"nadir/internal/search"
	"nadir/internal/store"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func Server(ctx context.Context, cfg *config.Config) error {
	log, err := logger.New(cfg.Middleware.Logger.Level)
	if err != nil {
		return fmt.Errorf("create logger: %w", err)
	}
	defer log.Sync()
	startupCtx, startupCancel := context.WithTimeout(ctx, cfg.HTTP.StartupTimeout)
	defer startupCancel()

	deps := middleware.NewDependencies(middleware.DependenciesConfig{
		Logger: log,
	})

	// Shared gRPC connection to Qdrant: store and the semantic cache both
	// talk to the same address, so they reuse one connection instead of
	// each dialing their own.
	qdrantConn, err := grpc.NewClient(cfg.Qdrant.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Error("qdrant dial failed", zap.Error(err))
		return fmt.Errorf("qdrant dial: %w", err)
	}
	defer qdrantConn.Close()
	qdrantClients := qdrantutil.NewClients(qdrantConn)

	s, err := store.NewDependencies(store.DependenciesConfig{
		Clients:     qdrantClients,
		Collection:  cfg.Qdrant.Collection,
		PrefetchMul: cfg.Qdrant.PrefetchMul,
	})
	if err != nil {
		log.Error("qdrant init failed", zap.Error(err))
		return fmt.Errorf("qdrant init: %w", err)
	}

	e := embedder.NewDependencies(embedder.DependenciesConfig{
		Addr:           cfg.Embedder.OllamaAddr,
		Model:          cfg.Embedder.Model,
		Dimensions:     cfg.Embedder.Dimensions,
		RequestTimeout: cfg.Embedder.RequestTimeout,
	})

	if err := s.EnsureCollection(startupCtx, e.Dimensions()); err != nil {
		log.Error("qdrant ensure collection failed", zap.Error(err))
		return fmt.Errorf("qdrant ensure collection: %w", err)
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
		Workers:          cfg.Ingest.Workers,
		MaxFileBytes:     cfg.Ingest.MaxFileBytes,
		EmbedBatchSize:   cfg.Ingest.EmbedBatchSize,
		MaxChunksPerFile: cfg.Ingest.MaxChunksPerFile,
		DocumentPrefix:   cfg.Embedder.DocumentPrefix,
		Log:              log,
	})
	if cfg.Docling.Enabled {
		ingestDeps.WithDocumentConverter(ingest.NewDoclingConverter(cfg.Docling.Addr, cfg.Docling.RequestTimeout))
		log.Info("PDF document intake enabled", zap.String("addr", cfg.Docling.Addr))
	}

	// Index-time LLM enrichment (HyPE questions, contextual intros): both
	// are one-time costs per chunk at ingest, zero query-time latency.
	// Enabling either after a collection was already ingested requires a
	// reindex to take effect.
	if cfg.Enrichment.Hype.Enabled || cfg.Enrichment.Contextual.Enabled {
		enrichmentEndpoint := cfg.EnrichmentEndpoint()
		ingestDeps.WithEnrichment(
			enrichment.NewDependencies(enrichment.DependenciesConfig{
				Addr:           enrichmentEndpoint.Addr,
				Model:          enrichmentEndpoint.Model,
				RequestTimeout: cfg.Enrichment.RequestTimeout,
			}),
			cfg.Enrichment.Hype.QuestionsPerChunk,
			cfg.Enrichment.Contextual.Enabled,
		)
		log.Info("index-time LLM enrichment enabled",
			zap.Bool("hype", cfg.Enrichment.Hype.Enabled),
			zap.Int("questions_per_chunk", cfg.Enrichment.Hype.QuestionsPerChunk),
			zap.Bool("contextual", cfg.Enrichment.Contextual.Enabled),
			zap.String("model", enrichmentEndpoint.Model),
			zap.String("addr", enrichmentEndpoint.Addr))
	}

	searchService := search.NewDependencies(search.DependenciesConfig{
		Embedder:               e,
		Store:                  s,
		QueryPrefix:            cfg.Embedder.QueryPrefix,
		MaxQueryChars:          cfg.Search.MaxQueryChars,
		MaxFragments:           cfg.Search.MaxFragments,
		MaxConcurrentFragments: cfg.Search.MaxConcurrentFragments,
		MaxTopK:                cfg.Search.MaxTopK,
		MaxChunksPerFile:       cfg.Search.MaxChunksPerFile,
		Log:                    log,
	})

	if cfg.Reranker.Enabled {
		searchService.WithReranker(reranker.NewDependencies(reranker.DependenciesConfig{
			Addr:           cfg.Reranker.Addr,
			MaxConcurrent:  cfg.Reranker.MaxConcurrent,
			RequestTimeout: cfg.Reranker.RequestTimeout,
		}), cfg.Reranker.CandidateMul)
		log.Info("cross-encoder reranker enabled", zap.String("addr", cfg.Reranker.Addr))
	}

	var gen generator.Generator
	if cfg.Generator.Enabled {
		generatorEndpoint := cfg.GeneratorEndpoint()
		gen = generator.NewDependencies(generator.DependenciesConfig{
			Addr:           generatorEndpoint.Addr,
			Model:          generatorEndpoint.Model,
			RequestTimeout: cfg.Generator.RequestTimeout,
		})
		log.Info("LLM generator enabled",
			zap.String("model", cfg.Generator.Model),
		)
	}

	var semanticCache cache.SemanticCache

	if cfg.SemanticCache.Enabled {
		var err error
		semanticCache, err = cache.NewDependencies(cache.DependenciesConfig{
			Clients:     qdrantClients,
			Collection:  cfg.SemanticCache.Collection,
			Embedder:    e,
			Threshold:   cfg.SemanticCache.Threshold,
			TTL:         cfg.SemanticCache.TTL,
			QueryPrefix: cfg.Embedder.QueryPrefix,
			Version: "v1:" + cfg.Embedder.Model + ":" + fmt.Sprint(cfg.Embedder.Dimensions) +
				":" + cfg.Embedder.QueryPrefix + ":" + cfg.Embedder.DocumentPrefix,
		})
		if err != nil {
			log.Error("semantic cache init failed", zap.Error(err))
		} else {
			if err := semanticCache.EnsureCollection(startupCtx); err != nil {
				log.Error("semantic cache ensure collection failed", zap.Error(err))
			} else {
				searchService.WithSemanticCache(semanticCache)
				ingestDeps.WithSemanticCache(semanticCache)
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
			Clients:    qdrantClients,
			Collection: cfg.History.Collection,
			Embedder:   e,
		})
		if err != nil {
			log.Error("history init failed", zap.Error(err))
		} else if err := h.EnsureCollection(startupCtx); err != nil {
			log.Error("history ensure collection failed", zap.Error(err))
		} else {
			hist = h
			log.Info("chat history persistence enabled", zap.String("collection", cfg.History.Collection))
		}
	}

	// Composite data-reset rule: dropping the collection must also clear
	// the semantic cache, or it keeps serving stale results for deleted
	// content. Enforced once here at the composition root so every caller
	// of Store.DeleteAll gets it for free.
	storeSvc := store.Store(s)
	if semanticCache != nil {
		storeSvc = &cacheInvalidatingStore{Store: s, cache: semanticCache}
	}

	// Conversational query rewriting: follow-ups are rewritten into
	// standalone search queries against the session's recent turns before
	// retrieval (Rewrite-Retrieve-Read). Skipped when a session has no
	// prior turns; rewrite failures fall back to the raw query.
	var chatRewriter rewriter.Rewriter
	if cfg.Rewriter.Enabled {
		rewriteEndpoint := cfg.RewriterEndpoint()
		if rewriteEndpoint.Addr == "" || rewriteEndpoint.Model == "" {
			log.Warn("rewriter enabled but no Ollama addr/model resolved; follow-ups will not be rewritten",
				zap.String("addr", rewriteEndpoint.Addr), zap.String("model", rewriteEndpoint.Model))
		} else {
			chatRewriter = rewriter.NewDependencies(rewriter.DependenciesConfig{
				Addr:           rewriteEndpoint.Addr,
				Model:          rewriteEndpoint.Model,
				RequestTimeout: cfg.Rewriter.RequestTimeout,
			})
			log.Info("conversational query rewriting enabled",
				zap.String("model", rewriteEndpoint.Model),
				zap.String("addr", rewriteEndpoint.Addr),
				zap.Int("turns", cfg.Rewriter.Turns))
		}
	}

	chatService := chat.NewDependencies(chat.DependenciesConfig{
		Searcher:         searchService,
		Generator:        gen,
		History:          hist,
		Rewriter:         chatRewriter,
		RewriteTurns:     cfg.Rewriter.Turns,
		MaxContextTokens: cfg.Chat.MaxContextTokens,
		EventBuffer:      cfg.Chat.EventBuffer,
		MaxEventLogBytes: cfg.Chat.MaxEventLogBytes,
		MaxRetainedTurns: cfg.Chat.MaxRetainedTurns,
		FinishedTurnTTL:  cfg.Chat.FinishedTurnTTL,
		PersistTimeout:   cfg.Chat.PersistTimeout,
		Model:            cfg.Generator.Model,
		Log:              log,
	})

	apiDeps := api.NewDependencies(api.DependenciesConfig{
		Ingest:               ingestDeps,
		Store:                storeSvc,
		History:              hist,
		Chat:                 chatService,
		TopK:                 cfg.Qdrant.TopK,
		MaxTopK:              cfg.Search.MaxTopK,
		SourcePaths:          cfg.Source.Paths,
		SourceIgnorePatterns: cfg.Source.IgnorePatterns,
		MaxSourceFileBytes:   cfg.Ingest.MaxFileBytes,
		MaxUploadBytes:       cfg.Ingest.MaxUploadBytes,
		Log:                  log,
	})

	engine := gin.New()
	engine.Use(gin.Recovery(), middleware.RequestID, middleware.Timeout(cfg.Middleware.Timeout), deps.RequestLog())
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error("http server shutdown error", zap.Error(err))
		}
	}()

	log.Info("http server starting", zap.String("addr", srv.Addr))
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Error("http server error", zap.Error(err))
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}

// cacheInvalidatingStore decorates Store.DeleteAll so a full data reset
// also clears the semantic cache — otherwise the cache keeps serving
// results for content that no longer exists.
type cacheInvalidatingStore struct {
	store.Store
	cache cache.SemanticCache
}

func (d *cacheInvalidatingStore) DeleteAll(ctx context.Context) error {
	if err := d.Store.DeleteAll(ctx); err != nil {
		return err
	}
	if err := d.cache.Clear(ctx); err != nil {
		return fmt.Errorf("clear semantic cache: %w", err)
	}
	return nil
}
