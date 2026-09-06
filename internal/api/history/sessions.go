package history

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nadir/internal/history"
)

// SessionsView is the sidebar's chat list.
type SessionsView struct {
	Enabled  bool
	Sessions []history.Session
}

// HistorySessions renders the sidebar's chat list — loaded on page load and
// again whenever a turn's stream ends (nadir:turn-appended), so the list
// re-sorts live as conversations happen.
func (h *Handlers) HistorySessions(c *gin.Context) {
	view := SessionsView{Enabled: h.history != nil}
	if h.history != nil {
		sessions, err := h.history.ListSessions(c.Request.Context(), 50)
		if err != nil {
			h.log.Warn("history list sessions failed", zap.Error(err))
		} else {
			view.Sessions = sessions
		}
	}

	h.render.HTML(c, http.StatusOK, "history-sessions", view)
}

// HistorySessionDelete permanently removes a chat session and all of its
// turns, invoked from the sidebar's delete modal.
func (h *Handlers) HistorySessionDelete(c *gin.Context) {
	if h.history == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat history is disabled"})
		return
	}

	sessionID := c.Param("id")
	if err := h.history.DeleteSession(c.Request.Context(), sessionID); err != nil {
		h.log.Warn("history delete session failed", zap.String("session_id", sessionID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}
