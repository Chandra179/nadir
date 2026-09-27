package history

import (
	"nadir/internal/core/conversation/chat"
	conversationhistory "nadir/internal/core/conversation/history"

	"log/slog"
)

// DependenciesConfig groups the history transport dependencies.
type DependenciesConfig struct {
	History         conversationhistory.Reader
	Chat            chat.Chat
	SessionPageSize int
	Log             *slog.Logger
}
