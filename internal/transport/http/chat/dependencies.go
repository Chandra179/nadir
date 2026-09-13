package chat

import "nadir/internal/conversation/chat"

// DependenciesConfig groups the chat transport dependencies.
type DependenciesConfig struct {
	Chat    chat.Chat
	TopK    int
	MaxTopK int
}
