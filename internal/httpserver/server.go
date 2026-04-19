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

	embedder := pkb.NewOllamaEmbedder(cfg.Embedder.OllamaAddr, cfg.Embedder.Model, cfg.Embedder.Dimensions)

	if err := store.EnsureCollection(context.Background(), embedder.Dimensions()); err != nil {
		log.Error(context.Background(), "qdrant ensure collection failed", logger.Field{Key: "error", Value: err.Error()})
		return
	}

	chunker := pkb.NewRecursiveChunker(cfg.Chunker.ChunkSize, cfg.Chunker.ChunkOverlap)
	fetcher := pkb.NewLocalFetcher(cfg.KnowledgeBase.Path)

	pipeline := pkb.NewPipeline(chunker, embedder, store, pkb.PipelineConfig{
		MaxAttempts:     cfg.Retry.MaxAttempts,
		InitialInterval: cfg.Retry.InitialInterval,
		MaxInterval:     cfg.Retry.MaxInterval,
		Multiplier:      cfg.Retry.Multiplier,
	})
	lister := pkb.NewLocalFileLister(cfg.KnowledgeBase.Path, cfg.PKB.IgnorePatterns)
	searchHandler := pkb.NewSearchHandler(embedder, store, cfg.Qdrant.TopK)
	ingestHandler := pkb.NewIngestHandler(lister, pipeline, fetcher, store, embedder, log)

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
