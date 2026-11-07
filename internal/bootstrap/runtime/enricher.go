package runtime

import (
	"fmt"

	"nadir/internal/bootstrap/configuration"
	"nadir/internal/core/documents/enrichment"
	cloudflarechat "nadir/internal/providers/cloudflare/chat"
	ollamachat "nadir/internal/providers/ollama/chat"
)

const (
	// contextualMaxTokens bounds one contextual chunk intro (a sentence under 30
	// words) while leaving slack for models that emit a little preamble.
	contextualMaxTokens = 256
	// contextualTemperature keeps intros close to the chunk with slight variety.
	contextualTemperature = 0.2
)

// newEnricher builds the contextual-retrieval enricher for the configured
// provider. It returns nil when contextual enrichment is disabled.
func newEnricher(cfg *config.Config) (enrichment.Enricher, error) {
	contextual := cfg.Enrichment.Contextual
	if !contextual.Enabled {
		return nil, nil
	}
	if contextual.Provider == "cloudflare" {
		client, err := cloudflarechat.New(cloudflarechat.Config{
			AccountID:      cfg.Cloudflare.AccountID,
			APIToken:       cfg.Cloudflare.APIToken,
			Model:          contextual.Model,
			RequestTimeout: cfg.Enrichment.RequestTimeout,
		})
		if err != nil {
			return nil, fmt.Errorf("cloudflare contextual enrichment: %w", err)
		}
		return enrichment.NewLLM(client.Completer(contextualMaxTokens, contextualTemperature)), nil
	}
	return enrichment.NewLLM(ollamachat.New(ollamachat.Config{
		Addr:           contextual.OllamaAddr,
		Model:          contextual.Model,
		Think:          contextual.Think,
		KeepAlive:      cfg.Inference.Ollama.KeepAlive.String(),
		RequestTimeout: cfg.Enrichment.RequestTimeout,
		Temperature:    contextualTemperature,
	}).Complete), nil
}
