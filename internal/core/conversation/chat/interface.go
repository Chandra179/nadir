package chat

import "context"

// Chat is consumed by the API layer. Generation is owned by the service:
// StartTurn kicks it off on its own goroutine, and any number of transport
// connections subscribe to the turn's event log — late subscribers are
// replayed from their cursor.
type Chat interface {
	StartTurn(ctx context.Context, req Request) Turn
	// Subscribe attaches to a turn's event log, replaying everything after
	// since (0 = from the beginning). ok is false for unknown turn ids.
	Subscribe(ctx context.Context, turnID string, since int64) (<-chan TurnEvent, func(), bool)
	// CancelTurn aborts an in-flight generation; the partial answer is kept
	// and persisted. ok is false for unknown turn ids.
	CancelTurn(turnID string) bool
	// DeleteSession removes a session and prevents in-flight or detached chat
	// work from recreating its turns.
	DeleteSession(ctx context.Context, sessionID string) error
	// DeleteAllSessions removes every persisted session and turn and cancels
	// active generations before the destructive operation.
	DeleteAllSessions(ctx context.Context) error
}
