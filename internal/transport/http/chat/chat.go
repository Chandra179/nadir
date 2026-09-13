// Package chat is the HTTP transport for the chat turn lifecycle: start a
// turn (POST /api/v1/turns), observe its event stream (SSE), cancel it.
// It parses requests and maps domain results to JSON — generation ownership,
// persistence and cancellation semantics live in internal/conversation/chat
// (ADR 0006).
package chat

import (
	"nadir/internal/conversation/chat"
)

// Handlers serves the turn lifecycle endpoints.
type Handlers struct {
	chat    chat.Chat
	topK    int
	maxTopK int
}

// NewDependencies builds the turn handlers.
func NewDependencies(cfg DependenciesConfig) *Handlers {
	maxTopK := cfg.MaxTopK
	if maxTopK <= 0 {
		maxTopK = 50
	}
	return &Handlers{chat: cfg.Chat, topK: cfg.TopK, maxTopK: maxTopK}
}
