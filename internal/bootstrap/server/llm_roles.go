package server

import (
	"fmt"
	"log/slog"

	"nadir/internal/bootstrap/configuration"
	"nadir/internal/core/conversation/generation"
	"nadir/internal/core/conversation/rewriting"
	"nadir/internal/core/observability"
	cloudflarechat "nadir/internal/providers/cloudflare/chat"
	cloudflaregenerator "nadir/internal/providers/cloudflare/generator"
	ollamachat "nadir/internal/providers/ollama/chat"
	ollamagenerator "nadir/internal/providers/ollama/generator"
)

// rewriteMaxTokens bounds one query rewrite, matching the Ollama num_predict.
const rewriteMaxTokens = 128

// newGenerator builds the answer generator for the configured provider. It
// returns nil when generation is disabled.
func newGenerator(cfg *config.Config, telemetry *observability.Recorder, log *slog.Logger) (generation.Generator, error) {
	if !cfg.Generator.Enabled {
		return nil, nil
	}
	log = log.With(slog.String("provider", cfg.Generator.Provider), slog.String("model", cfg.Generator.Model))
	if cfg.Generator.Provider == "cloudflare" {
		gen, err := cloudflaregenerator.NewDependencies(cloudflaregenerator.DependenciesConfig{
			AccountID:       cfg.Cloudflare.AccountID,
			APIToken:        cfg.Cloudflare.APIToken,
			Model:           cfg.Generator.Model,
			RequestTimeout:  cfg.Generator.RequestTimeout,
			MaxOutputTokens: cfg.Generator.MaxOutputTokens,
			Telemetry:       telemetry,
		})
		if err != nil {
			return nil, fmt.Errorf("cloudflare generator: %w", err)
		}
		log.Info("LLM generator enabled")
		return gen, nil
	}
	endpoint := cfg.GeneratorEndpoint()
	log.Info("LLM generator enabled")
	return ollamagenerator.NewDependencies(ollamagenerator.DependenciesConfig{
		Addr:           endpoint.Addr,
		Model:          endpoint.Model,
		Think:          cfg.Generator.Think,
		RequestTimeout: cfg.Generator.RequestTimeout,
		KeepAlive:      cfg.Inference.Ollama.KeepAlive.String(),
		Options: map[string]any{
			"temperature": 0,
			"num_predict": cfg.Generator.MaxOutputTokens,
			"num_ctx":     cfg.Generator.NumCtx,
		},
		Telemetry: telemetry,
	}), nil
}

// newRewriter builds the conversational query rewriter for the configured
// provider. It returns nil when rewriting is disabled or an Ollama endpoint is
// unresolved (follow-ups are then searched as asked).
func newRewriter(cfg *config.Config, log *slog.Logger) (rewriting.Rewriter, error) {
	if !cfg.Rewriter.Enabled {
		return nil, nil
	}
	var complete rewriting.Completer
	if cfg.Rewriter.Provider == "cloudflare" {
		client, err := cloudflarechat.New(cloudflarechat.Config{
			AccountID:      cfg.Cloudflare.AccountID,
			APIToken:       cfg.Cloudflare.APIToken,
			Model:          cfg.Rewriter.Model,
			RequestTimeout: cfg.Rewriter.RequestTimeout,
		})
		if err != nil {
			return nil, fmt.Errorf("cloudflare rewriter: %w", err)
		}
		complete = client.Completer(rewriteMaxTokens, 0)
	} else {
		endpoint := cfg.RewriterEndpoint()
		if endpoint.Addr == "" || endpoint.Model == "" {
			log.Warn("rewriter enabled but no Ollama addr/model resolved; follow-ups will not be rewritten",
				slog.String("addr", endpoint.Addr), slog.String("model", endpoint.Model))
			return nil, nil
		}
		complete = ollamachat.New(ollamachat.Config{
			Addr:           endpoint.Addr,
			Model:          endpoint.Model,
			Think:          cfg.Rewriter.Think,
			KeepAlive:      cfg.Inference.Ollama.KeepAlive.String(),
			RequestTimeout: cfg.Rewriter.RequestTimeout,
			MaxTokens:      rewriteMaxTokens,
		}).Complete
	}
	log.Info("conversational query rewriting enabled",
		slog.String("provider", cfg.Rewriter.Provider),
		slog.String("model", cfg.Rewriter.Model),
		slog.Int("turns", cfg.Rewriter.Turns))
	return rewriting.NewLLM(complete), nil
}
