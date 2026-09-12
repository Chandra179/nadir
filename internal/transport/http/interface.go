package api

import (
	"context"

	"nadir/internal/conversation/history"
)

// documentResetter is the only Document Store capability needed by the
// transport. Retrieval and indexing receive their own narrower seams.
type documentResetter interface {
	DeleteAll(ctx context.Context) error
}

// sessionReader is the read-only history capability used by the page shell
// and sidebar. Chat mutations are owned by the Chat Module.
type sessionReader interface {
	ListSessions(ctx context.Context, limit int) ([]history.Session, error)
	ListTurns(ctx context.Context, sessionID string) ([]history.Turn, error)
	GetSession(ctx context.Context, sessionID string) (history.Session, error)
}
