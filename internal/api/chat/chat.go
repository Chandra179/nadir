// Package chat is the HTTP transport for the chat turn lifecycle: start a
// turn (POST /retrieval/search), observe its event stream (SSE), cancel it.
// It parses requests and renders views only — generation ownership,
// persistence and cancellation semantics live in internal/chat (ADR 0006).
package chat

import (
	"nadir/internal/api/internal/render"
	"nadir/internal/chat"
)

// Config wires the turn handlers.
type Config struct {
	// Chat is the chat use-case.
	Chat chat.Chat
	// TopK is the resolved default result count for requests that omit one.
	TopK int
	// Render renders the UI templates.
	Render *render.Engine
}

// Handlers serves the turn lifecycle endpoints.
type Handlers struct {
	chat   chat.Chat
	topK   int
	render *render.Engine
}

// New builds the turn handlers.
func New(cfg Config) *Handlers {
	return &Handlers{chat: cfg.Chat, topK: cfg.TopK, render: cfg.Render}
}
