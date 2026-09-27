// Package history is the HTTP transport for chat history management.
package history

import (
	"io"
	"log/slog"

	"nadir/internal/core/conversation/chat"
	conversationhistory "nadir/internal/core/conversation/history"
)

// Handlers serves the history endpoints.
type Handlers struct {
	history         conversationhistory.Reader
	chat            chat.Chat
	sessionPageSize int
	log             *slog.Logger
}

// NewDependencies builds the history handlers; a nil logger disables logging.
func NewDependencies(cfg DependenciesConfig) *Handlers {
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	sessionPageSize := cfg.SessionPageSize
	if sessionPageSize <= 0 {
		sessionPageSize = 50
	}
	return &Handlers{history: cfg.History, chat: cfg.Chat, sessionPageSize: sessionPageSize, log: log}
}
