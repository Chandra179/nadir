package server

import (
	"context"
	"fmt"
	"net/http"

	"nadir/internal/bootstrap/assembly"
	"nadir/internal/bootstrap/configuration"
	"nadir/internal/bootstrap/httpmiddleware"
	"nadir/internal/bootstrap/logging"
	"nadir/internal/bootstrap/readiness"
	"nadir/internal/bootstrap/runtime"
	"nadir/internal/core/conversation/generation"
	ollamagenerator "nadir/internal/providers/ollama/generator"
	ollamarewriter "nadir/internal/providers/ollama/rewriter"
	qdranthistory "nadir/internal/providers/qdrant/history"

	"log/slog"
)

// Server builds and runs the API HTTP process until ctx is cancelled.
func Server(ctx context.Context, cfg *config.Config) error {
	log, err := logger.New(cfg.Middleware.Logger.Level)
	if err != nil {
		return fmt.Errorf("create logger: %w", err)
	}
	startupCtx, startupCancel := context.WithTimeout(ctx, cfg.HTTP.StartupTimeout)
	defer startupCancel()

	deps := middleware.NewDependencies(middleware.DependenciesConfig{
		Logger: log,
	})

	graph, err := runtime.NewDependencies(startupCtx, cfg, log)
	if err != nil {
		log.Error("shared runtime init failed", slog.Any("error", err))
		return fmt.Errorf("shared runtime: %w", err)
	}
	defer func() {
		if err := graph.Close(); err != nil {
			log.Warn("shared runtime close failed", slog.Any("error", err))
		}
	}()

	var gen generation.Generator
	if cfg.Generator.Enabled {
		generatorEndpoint := cfg.GeneratorEndpoint()
		gen = ollamagenerator.NewDependencies(ollamagenerator.DependenciesConfig{
			Addr:           generatorEndpoint.Addr,
			Model:          generatorEndpoint.Model,
			Think:          cfg.Generator.Think,
			RequestTimeout: cfg.Generator.RequestTimeout,
			KeepAlive:      cfg.Inference.Ollama.KeepAlive.String(),
			Options: map[string]any{
				"temperature": 0,
				"num_predict": cfg.Generator.MaxOutputTokens,
				"num_ctx":     cfg.Generator.NumCtx,
			},
			Telemetry: graph.Telemetry,
		})
		log.Info("LLM generator enabled",
			slog.String("model", cfg.Generator.Model),
		)
	}

	prov := assembly.APIProviders{Generator: gen}

	if cfg.History.Enabled {
		h, err := qdranthistory.NewDependencies(qdranthistory.DependenciesConfig{
			Clients:      graph.Clients,
			Collection:   cfg.History.Collection,
			Dimensions:   graph.Embedder.Dimensions(),
			TurnPageSize: cfg.History.TurnPageSize,
		})
		if err != nil {
			log.Error("history init failed", slog.Any("error", err))
		} else if err := h.EnsureCollection(startupCtx); err != nil {
			log.Error("history ensure collection failed", slog.Any("error", err))
		} else {
			// The concrete Adapter satisfies each consumer's narrow, private
			// Interface. No aggregate history Interface is needed here.
			prov.History = h
			log.Info("chat history persistence enabled", slog.String("collection", cfg.History.Collection))
		}
	}

	// Conversational query rewriting: follow-ups are rewritten into
	// standalone search queries against the session's recent turns before
	// retrieval (Rewrite-Retrieve-Read). Skipped when a session has no
	// prior turns; rewrite failures fall back to the raw query.
	if cfg.Rewriter.Enabled {
		rewriteEndpoint := cfg.RewriterEndpoint()
		if rewriteEndpoint.Addr == "" || rewriteEndpoint.Model == "" {
			log.Warn("rewriter enabled but no Ollama addr/model resolved; follow-ups will not be rewritten",
				slog.String("addr", rewriteEndpoint.Addr), slog.String("model", rewriteEndpoint.Model))
		} else {
			prov.Rewriter = ollamarewriter.NewDependencies(ollamarewriter.DependenciesConfig{
				Addr:           rewriteEndpoint.Addr,
				Model:          rewriteEndpoint.Model,
				Think:          cfg.Rewriter.Think,
				RequestTimeout: cfg.Rewriter.RequestTimeout,
				KeepAlive:      cfg.Inference.Ollama.KeepAlive.String(),
			})
			log.Info("conversational query rewriting enabled",
				slog.String("model", rewriteEndpoint.Model),
				slog.String("addr", rewriteEndpoint.Addr),
				slog.Int("turns", cfg.Rewriter.Turns))
		}
	}

	prov.Readiness = []readiness.Dependency{
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
			Name:     "generator",
			Probe:    modelReadiness(cfg.Generator.OllamaAddr, cfg.Generator.Model, cfg.HTTP.ReadinessTimeout),
			Disabled: !cfg.Generator.Enabled,
		},
		{
			Name:     "rewriter",
			Probe:    modelReadiness(cfg.Rewriter.OllamaAddr, cfg.Rewriter.Model, cfg.HTTP.ReadinessTimeout),
			Disabled: !cfg.Rewriter.Enabled || !cfg.History.Enabled,
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
	}

	built := assembly.BuildAPI(cfg, graph.Runtime, prov, log)
	mux := built.Mux
	mux.Handle("/debug/metrics", metricsHandler(graph.Telemetry))
	var handler http.Handler = mux
	handler = deps.RequestLog(handler)
	handler = middleware.Timeout(cfg.Middleware.Timeout)(handler)
	handler = middleware.RequestID(handler)
	handler = middleware.Recovery(log)(handler)

	srv := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      handler,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,
	}

	log.Info("http server starting", slog.String("addr", srv.Addr))
	return runHTTPServer(ctx, srv, cfg.HTTP.ShutdownTimeout, srv.ListenAndServe, built.Drain, log)
}
