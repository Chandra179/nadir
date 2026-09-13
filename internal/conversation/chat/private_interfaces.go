package chat

import (
	"context"

	"nadir/internal/conversation/history"
)

// historyStore is the Chat lifecycle's narrow persistence seam. Sidebar
// listing and unrelated session lookups stay outside the Chat Module.
type historyStore interface {
	CreateSession(ctx context.Context, title string) (history.Session, error)
	TruncateSession(ctx context.Context, sessionID string, beforeSequence int) error
	AppendTurn(ctx context.Context, sessionID string, turn history.Turn, firstTurnTitle string) error
	// ListTurns returns a session's turns in sequence order; read when
	// rewriting a follow-up query against prior conversation context.
	ListTurns(ctx context.Context, sessionID string) ([]history.Turn, error)
	DeleteSession(ctx context.Context, sessionID string) error
	DeleteAllSessions(ctx context.Context) error
}
