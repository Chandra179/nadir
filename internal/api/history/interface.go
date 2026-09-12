package history

import (
	"context"

	domainhistory "nadir/internal/history"
)

// sessionLister is the only persistence capability needed by the sidebar.
// Session mutations are routed through the Chat lifecycle instead.
type sessionLister interface {
	ListSessions(ctx context.Context, limit int) ([]domainhistory.Session, error)
}
