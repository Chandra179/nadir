// Package history is the HTTP transport for chat history management.
package history

import (
	"go.uber.org/zap"

	"nadir/internal/conversation/chat"
	conversationhistory "nadir/internal/conversation/history"
)

// Handlers serves the history endpoints.
type Handlers struct {
	history         conversationhistory.Reader
	chat            chat.Chat
	sessionPageSize int
	log             *zap.Logger
}

// NewDependencies builds the history handlers; a nil logger disables logging.
func NewDependencies(cfg DependenciesConfig) *Handlers {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	sessionPageSize := cfg.SessionPageSize
	if sessionPageSize <= 0 {
		sessionPageSize = 50
	}
	return &Handlers{history: cfg.History, chat: cfg.Chat, sessionPageSize: sessionPageSize, log: log}
}
