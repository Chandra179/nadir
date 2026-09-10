package history

import "context"

// History persists chat sessions and their turns. Callers must invoke
// AppendTurn best-effort and non-blocking — a degraded store must never
// affect the live chat response.
type History interface {
	CreateSession(ctx context.Context, title string) (Session, error)
	// TruncateSession removes the turn at beforeSequence and every later
	// turn, preserving the ordered prefix in the existing session.
	TruncateSession(ctx context.Context, sessionID string, beforeSequence int) error
	// AppendTurn creates the session on the fly (using firstTurnTitle) if
	// sessionID doesn't exist yet — a defensive fallback for the common
	// path of CreateSession having already run.
	AppendTurn(ctx context.Context, sessionID string, turn Turn, firstTurnTitle string) error
	ListSessions(ctx context.Context, limit int) ([]Session, error)
	GetSession(ctx context.Context, sessionID string) (Session, error)
	ListTurns(ctx context.Context, sessionID string) ([]Turn, error)
	// DeleteSession removes a session and all of its turns.
	DeleteSession(ctx context.Context, sessionID string) error
	// DeleteAllSessions removes every persisted chat session and turn.
	DeleteAllSessions(ctx context.Context) error
}
