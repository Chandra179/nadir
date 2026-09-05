package history

import "context"

// History persists chat sessions and their turns. Callers are expected to
// invoke AppendTurn in a best-effort, non-blocking manner (see
// internal/api/retrieval.go) — a degraded or unreachable store must never
// affect the live chat response.
type History interface {
	CreateSession(ctx context.Context, title string) (Session, error)
	// AppendTurn creates the session on the fly (using firstTurnTitle) if
	// sessionID doesn't exist yet — a defensive fallback for the common
	// path of CreateSession having already run.
	AppendTurn(ctx context.Context, sessionID string, turn Turn, firstTurnTitle string) error
	ListSessions(ctx context.Context, limit int) ([]Session, error)
	GetSession(ctx context.Context, sessionID string) (Session, error)
	ListTurns(ctx context.Context, sessionID string) ([]Turn, error)
	// DeleteSession removes a session and all of its turns.
	DeleteSession(ctx context.Context, sessionID string) error
}