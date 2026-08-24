package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nadir/internal/history"
)

type historySessionsView struct {
	Enabled  bool
	Sessions []history.Session
}

// HistorySessions renders the sidebar's chat list — loaded on page load and
// again whenever RetrievalSearch fires the nadir:turn-appended event, so
// the list re-sorts live as conversations happen.
func (d *dependencies) HistorySessions(c *gin.Context) {
	view := historySessionsView{Enabled: d.history != nil}
	if d.history != nil {
		sessions, err := d.history.ListSessions(c.Request.Context(), 50)
		if err != nil {
			d.log.Warn("history list sessions failed", zap.Error(err))
		} else {
			view.Sessions = sessions
		}
	}

	d.renderHTML(c, http.StatusOK, "history-sessions", view)
}
