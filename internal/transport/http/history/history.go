// Package history is the HTTP transport for chat history management.
package history

import (
	"go.uber.org/zap"

	"nadir/internal/conversation/chat"
)

// Config wires the history handlers.
type Config struct {
	// History is the persistence use-case; optional when history is disabled.
	History sessionReader
	// Log receives handler failures.
	Log *zap.Logger
	// Chat owns generation and history mutation coordination. Destructive
	// operations must go through it so in-flight turns cannot reappear.
	Chat chat.Chat
}

// Handlers serves the history endpoints.
type Handlers struct {
	history sessionReader
	chat    chat.Chat
	log     *zap.Logger
}

// New builds the history handlers; a nil logger disables logging.
func New(cfg Config) *Handlers {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &Handlers{history: cfg.History, chat: cfg.Chat, log: log}
}
