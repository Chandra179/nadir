package history

import (
	"nadir/internal/conversation/chat"
	conversationhistory "nadir/internal/conversation/history"

	"go.uber.org/zap"
)

// DependenciesConfig groups the history transport dependencies.
type DependenciesConfig struct {
	History         conversationhistory.Reader
	Chat            chat.Chat
	SessionPageSize int
	Log             *zap.Logger
}
