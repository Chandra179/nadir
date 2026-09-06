// Package history is the HTTP transport for chat history management: the
// sidebar's session list and session deletion. Session page replay is part
// of the page shell (package api); persistence lives in internal/history.
package history

import (
	"go.uber.org/zap"

	"nadir/internal/api/internal/render"
	"nadir/internal/history"
)

// Config wires the history handlers.
type Config struct {
	// History is the persistence use-case; optional — when nil, the sidebar
	// renders empty and deletion reports "disabled".
	History history.History
	// Render renders the UI templates.
	Render *render.Engine
	// Log receives handler failures.
	Log *zap.Logger
}

// Handlers serves the history endpoints.
type Handlers struct {
	history history.History
	render  *render.Engine
	log     *zap.Logger
}

// New builds the history handlers; a nil logger disables logging.
func New(cfg Config) *Handlers {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &Handlers{history: cfg.History, render: cfg.Render, log: log}
}
