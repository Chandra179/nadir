package api

import (
	"html/template"
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
	tmpl, err := template.ParseFiles("dashboard/partials/history_sessions.html")
	if err != nil {
		d.log.Error("parse history sessions template failed", zap.Error(err))
		c.String(http.StatusInternalServerError, "")
		return
	}

	view := historySessionsView{Enabled: d.history != nil}
	if d.history != nil {
		sessions, err := d.history.ListSessions(c.Request.Context(), 50)
		if err != nil {
			d.log.Warn("history list sessions failed", zap.Error(err))
		} else {
			view.Sessions = sessions
		}
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(c.Writer, "history-sessions", view); err != nil {
		d.log.Error("render history sessions fragment failed", zap.Error(err))
	}
}
