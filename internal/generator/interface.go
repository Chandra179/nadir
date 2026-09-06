package generator

import "context"

// Event is one item of a generation stream. Sealed: the concrete set is
// Token, Error, Done.
type Event interface{ isEvent() }

// TokenEvent carries one chunk of generated answer text.
type TokenEvent struct{ Text string }

// ErrorEvent aborts a stream: generation failed mid-flight.
type ErrorEvent struct{ Err error }

// DoneEvent marks a successful end of generation.
type DoneEvent struct{}

func (TokenEvent) isEvent() {}
func (ErrorEvent) isEvent() {}
func (DoneEvent) isEvent()  {}

// Generator produces a stream of events for a fully-built prompt. Prompt
// construction is the caller's (use-case) concern; this is a dumb LLM
// transport, symmetric with the embedder and reranker adapters.
type Generator interface {
	Generate(ctx context.Context, prompt string) (<-chan Event, error)
}
