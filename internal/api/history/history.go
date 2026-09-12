// Package history is the HTTP transport for chat history management: the
// sidebar's session list and session deletion. Session page replay is part
// of the page shell (package api); persistence lives in internal/history.
package history

import (
	"go.uber.org/zap"

	"nadir/internal/api/render"
	"nadir/internal/chat"
)

// Config wires the history handlers.
type Config struct {
	// History is the persistence use-case; optional — when nil, the sidebar
	// renders empty and deletion reports "disabled".
	History sessionLister
	// Render renders the UI templates.
	Render *render.Engine
	// Log receives handler failures.
	Log *zap.Logger
	// Chat owns generation and history mutation coordination. Destructive
	// operations must go through it so in-flight turns cannot reappear.
	Chat chat.Chat
}

// Handlers serves the history endpoints.
type Handlers struct {
	history sessionLister
	chat    chat.Chat
	render  *render.Engine
	log     *zap.Logger
}

// New builds the history handlers; a nil logger disables logging.
func New(cfg Config) *Handlers {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &Handlers{history: cfg.History, chat: cfg.Chat, render: cfg.Render, log: log}
}
