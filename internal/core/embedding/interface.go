// Package embedding defines the provider-neutral embedding capability used by
// indexing, retrieval, cache, and history Modules.
package embedding

import "context"

// ProbeResult describes the embedding runtime observed by a provider's
// readiness probe. LoadedModel is empty for providers that cannot report
// residency.
type ProbeResult struct {
	ConfiguredModel string
	LoadedModel     string
	Dimensions      int
}

// Embedder produces vectors for one or more inputs and reports the configured
// vector size. Providers such as Ollama implement this contract in Adapters.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dimensions() int
}
