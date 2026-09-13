package generation

import "context"

// Generator produces a stream of events for a fully-built prompt. Prompt
// construction is the caller's (use-case) concern; this is a dumb LLM
// transport, symmetric with the embedder and reranker adapters.
type Generator interface {
	Generate(ctx context.Context, prompt string) (<-chan Event, error)
}
