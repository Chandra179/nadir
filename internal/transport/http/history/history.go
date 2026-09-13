// Package history is the HTTP transport for chat history management.
package history

import (
	"go.uber.org/zap"

	"nadir/internal/conversation/chat"
	conversationhistory "nadir/internal/conversation/history"
)

// Handlers serves the history endpoints.
type Handlers struct {
	history conversationhistory.Reader
	chat    chat.Chat
	log     *zap.Logger
}

// NewDependencies builds the history handlers; a nil logger disables logging.
func NewDependencies(cfg DependenciesConfig) *Handlers {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &Handlers{history: cfg.History, chat: cfg.Chat, log: log}
}
