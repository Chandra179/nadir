package server

import (
	"context"
	"fmt"
	"net/http"

	ollamagenerator "nadir/internal/adapters/ollama/generator"
	ollamarewriter "nadir/internal/adapters/ollama/rewriter"
	qdranthistory "nadir/internal/adapters/qdrant/history"
	"nadir/internal/conversation/chat"
	"nadir/internal/conversation/generation"
	"nadir/internal/platform/configuration"
	"nadir/internal/platform/httpmiddleware"
	"nadir/internal/platform/logging"
	"nadir/internal/platform/readiness"
	"nadir/internal/platform/runtime"
	"nadir/internal/retrieval/rewriting"
	"nadir/internal/transport/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Server builds and runs the API HTTP process until ctx is cancelled.
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

	graph, err := runtime.NewDependencies(startupCtx, cfg, log, runtime.Options{})
	if err != nil {
		log.Error("shared runtime init failed", zap.Error(err))
		return fmt.Errorf("shared runtime: %w", err)
	}
	defer func() {
		if err := graph.Close(); err != nil {
			log.Warn("shared runtime close failed", zap.Error(err))
		}
	}()

	var gen generation.Generator
	if cfg.Generator.Enabled {
		generatorEndpoint := cfg.GeneratorEndpoint()
		gen = ollamagenerator.NewDependencies(ollamagenerator.DependenciesConfig{
			Addr:           generatorEndpoint.Addr,
			Model:          generatorEndpoint.Model,
			RequestTimeout: cfg.Generator.RequestTimeout,
		})
		log.Info("LLM generator enabled",
			zap.String("model", cfg.Generator.Model),
		)
	}

	chatConfig := chat.DependenciesConfig{
		Searcher:         graph.Searcher,
		Generator:        gen,
		RewriteTurns:     cfg.Rewriter.Turns,
		MaxContextTokens: cfg.Chat.MaxContextTokens,
		EventBuffer:      cfg.Chat.EventBuffer,
		MaxEventLogBytes: cfg.Chat.MaxEventLogBytes,
		MaxRetainedTurns: cfg.Chat.MaxRetainedTurns,
		FinishedTurnTTL:  cfg.Chat.FinishedTurnTTL,
		PersistTimeout:   cfg.Chat.PersistTimeout,
		Model:            cfg.Generator.Model,
		Log:              log,
	}

	apiConfig := api.DependenciesConfig{
		Ingest:               graph.Ingest,
		Reset:                graph.Reset,
		TopK:                 cfg.Qdrant.TopK,
		MaxTopK:              cfg.Search.MaxTopK,
		SourcePaths:          cfg.Source.Paths,
		SourceIgnorePatterns: cfg.Source.IgnorePatterns,
		MaxSourceFileBytes:   cfg.Ingest.MaxFileBytes,
		MaxUploadBytes:       cfg.Ingest.MaxUploadBytes,
		ReadinessTimeout:     cfg.HTTP.ReadinessTimeout,
		Log:                  log,
	}

	if cfg.History.Enabled {
		h, err := qdranthistory.NewDependencies(qdranthistory.DependenciesConfig{
			Clients:    graph.Clients,
			Collection: cfg.History.Collection,
			Embedder:   graph.Embedder,
		})
		if err != nil {
			log.Error("history init failed", zap.Error(err))
		} else if err := h.EnsureCollection(startupCtx); err != nil {
			log.Error("history ensure collection failed", zap.Error(err))
		} else {
			// The concrete Adapter satisfies each consumer's narrow, private
			// Interface. No aggregate history Interface is needed here.
			chatConfig.History = h
			apiConfig.History = h
			log.Info("chat history persistence enabled", zap.String("collection", cfg.History.Collection))
		}
	}

	// Conversational query rewriting: follow-ups are rewritten into
	// standalone search queries against the session's recent turns before
	// retrieval (Rewrite-Retrieve-Read). Skipped when a session has no
	// prior turns; rewrite failures fall back to the raw query.
	var chatRewriter rewriting.Rewriter
	if cfg.Rewriter.Enabled {
		rewriteEndpoint := cfg.RewriterEndpoint()
		if rewriteEndpoint.Addr == "" || rewriteEndpoint.Model == "" {
			log.Warn("rewriter enabled but no Ollama addr/model resolved; follow-ups will not be rewritten",
				zap.String("addr", rewriteEndpoint.Addr), zap.String("model", rewriteEndpoint.Model))
		} else {
			chatRewriter = ollamarewriter.NewDependencies(ollamarewriter.DependenciesConfig{
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

	chatConfig.Rewriter = chatRewriter
	chatService := chat.NewDependencies(chatConfig)

	apiConfig.Chat = chatService
	readinessChecker := readiness.NewDependencies(readiness.DependenciesConfig{Dependencies: []readiness.Dependency{
		{
			Name:  "qdrant",
			Probe: readiness.HealthProbe(graph.QdrantHealth),
		},
		{
			Name: "embedding",
			Probe: func(ctx context.Context) (readiness.Check, error) {
				result, err := graph.EmbeddingProbe(ctx)
				return readiness.Check{
					Model:       result.ConfiguredModel,
					LoadedModel: result.LoadedModel,
					Details:     fmt.Sprintf("dimensions=%d", result.Dimensions),
				}, err
			},
		},
		{
			Name: "reranker",
			Probe: func(ctx context.Context) (readiness.Check, error) {
				if graph.RerankerProbe == nil {
					return readiness.Check{}, nil
				}
				result, err := graph.RerankerProbe(ctx)
				return readiness.Check{
					Model:       result.ConfiguredModel,
					LoadedModel: result.LoadedModel,
					Backend:     result.Backend,
					Device:      result.Device,
				}, err
			},
			Disabled: graph.RerankerProbe == nil,
		},
	}})
	apiConfig.Readiness = func(ctx context.Context) api.ReadinessReport {
		report := readinessChecker.Check(ctx)
		checks := make(map[string]api.ReadinessCheck, len(report.Checks))
		for name, check := range report.Checks {
			checks[name] = api.ReadinessCheck{
				Ready:       check.Ready,
				Model:       check.Model,
				LoadedModel: check.LoadedModel,
				Backend:     check.Backend,
				Device:      check.Device,
				Error:       check.Error,
				Details:     check.Details,
			}
		}
		return api.ReadinessReport{Ready: report.Ready, Checks: checks}
	}
	apiDeps := api.NewDependencies(apiConfig)

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

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		log.Info("http server shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error("http server shutdown error", zap.Error(err))
		}
		if err := chatService.Drain(shutdownCtx); err != nil {
			log.Error("chat lifecycle drain failed", zap.Error(err))
		}
	}()

	log.Info("http server starting", zap.String("addr", srv.Addr))
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Error("http server error", zap.Error(err))
		return fmt.Errorf("http server: %w", err)
	}
	if ctx.Err() != nil {
		<-shutdownDone
	}
	return nil
}
