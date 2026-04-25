package httpserver

import (
	"context"
	"net/http"

	"nadir/config"
	"nadir/internal/middleware"
	"nadir/internal/pkb"

	"github.com/Chandra179/gosdk/logger"
)

func Server(cfg *config.Config) {
	log := logger.NewLogger(cfg.Middleware.Logger.Level)
	deps := middleware.NewDependencies(log)

	globalChain := func(h http.Handler) http.Handler {
		return middleware.Chain(h,
			deps.Recovery(),
			middleware.RequestID,
			middleware.Timeout(middleware.TimeoutConfig{Duration: cfg.Middleware.Timeout}),
		)
	}

	store, err := pkb.NewQdrantStore(cfg.Qdrant.Addr, cfg.Qdrant.Collection)
	if err != nil {
		log.Error(context.Background(), "qdrant init failed", logger.Field{Key: "error", Value: err.Error()})
		return
	}
	if cfg.SparseScorer.Provider == "splade" {
		store = store.WithSparseScorer(pkb.NewSPLADESparseScorer(cfg.SparseScorer.Addr))
		log.Info(context.Background(), "splade sparse scorer enabled", logger.Field{Key: "addr", Value: cfg.SparseScorer.Addr})
	}

	embedder := pkb.NewOllamaEmbedder(cfg.Embedder.OllamaAddr, cfg.Embedder.Model, cfg.Embedder.Dimensions)

	if err := store.EnsureCollection(context.Background(), embedder.Dimensions()); err != nil {
		log.Error(context.Background(), "qdrant ensure collection failed", logger.Field{Key: "error", Value: err.Error()})
		return
	}

	var chunker pkb.Chunker
	if cfg.Chunker.Provider == "sentence-window" {
		windowSize := cfg.Chunker.WindowSize
		if windowSize <= 0 {
			windowSize = 3
		}
		chunker = pkb.NewSentenceWindowChunker(windowSize)
		log.Info(context.Background(), "sentence-window chunker enabled", logger.Field{Key: "window_size", Value: windowSize})
	} else {
		chunker = pkb.NewRecursiveChunker(cfg.Chunker.ChunkSize, cfg.Chunker.ChunkOverlap)
	}
	fetcher := pkb.NewLocalFetcher(cfg.KnowledgeBase.Path)

	pipeline := pkb.NewPipeline(chunker, embedder, store, pkb.PipelineConfig{
		MaxAttempts:     cfg.Retry.MaxAttempts,
		InitialInterval: cfg.Retry.InitialInterval,
		MaxInterval:     cfg.Retry.MaxInterval,
		Multiplier:      cfg.Retry.Multiplier,
	})
	lister := pkb.NewLocalFileLister(cfg.KnowledgeBase.Path, cfg.PKB.IgnorePatterns)
	searchHandler := pkb.NewSearchHandler(embedder, store, cfg.Qdrant.TopK)
	if cfg.HyDE.Enabled {
		ollamaAddr := cfg.HyDE.OllamaAddr
		if ollamaAddr == "" {
			ollamaAddr = cfg.Embedder.OllamaAddr
		}
		hydeGen := pkb.NewOllamaHyDEGenerator(ollamaAddr, cfg.HyDE.Model)
		hydeSearcher := pkb.NewHyDESearcher(hydeGen, embedder, store, cfg.HyDE.NumDocs)
		searchHandler.WithHyDE(hydeSearcher)
		log.Info(context.Background(), "HyDE enabled",
			logger.Field{Key: "model", Value: cfg.HyDE.Model},
			logger.Field{Key: "num_docs", Value: cfg.HyDE.NumDocs},
		)
	}
	if cfg.Reranker.Enabled {
		mul := cfg.Reranker.CandidateMul
		if mul < 1 {
			mul = 3
		}
		searchHandler.WithReranker(pkb.NewHTTPReranker(cfg.Reranker.Addr), mul)
		log.Info(context.Background(), "cross-encoder reranker enabled", logger.Field{Key: "addr", Value: cfg.Reranker.Addr})
	}
	ingestHandler := pkb.NewIngestHandler(lister, pipeline, fetcher, store, log)

	mux := http.NewServeMux()
	mux.Handle("POST /search", globalChain(searchHandler))
	mux.Handle("POST /ingest", globalChain(ingestHandler))

	srv := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      mux,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,
	}
	log.Info(context.Background(), "http server starting", logger.Field{Key: "addr", Value: srv.Addr})
	if err := srv.ListenAndServe(); err != nil {
		log.Error(context.Background(), "http server error", logger.Field{Key: "error", Value: err.Error()})
	}
}
