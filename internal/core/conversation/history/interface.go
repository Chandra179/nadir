package history

import "context"

// Reader exposes the persisted history queries used by conversation and
// transport consumers. Session mutations remain owned by the Chat lifecycle.
type Reader interface {
	ListSessions(ctx context.Context, limit int) ([]Session, error)
	ListTurns(ctx context.Context, sessionID string) ([]Turn, error)
	GetSession(ctx context.Context, sessionID string) (Session, error)
}
