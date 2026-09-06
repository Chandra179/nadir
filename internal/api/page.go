// The page shell renders the full dashboard: the "page" template with the
// composer defaults and, when replaying a past conversation, its turns.
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	chatapi "nadir/internal/api/chat"
)

// pageView is the data passed to the "page" template: the composer's
// default top_k, and when replaying a past conversation the session id to
// keep appending to plus its turns to pre-render.
type pageView struct {
	DefaultTopK int
	SessionID   string
	Turns       []chatapi.TurnView
}

// Retrieval renders the chat page shell (sidebar + composer, no messages
// yet) — the composer posts to the turns transport, which appends one turn
// (question + trace + answer) as an HTML fragment via htmx.
func (d *dependencies) Retrieval(c *gin.Context) {
	d.renderPage(c, pageView{DefaultTopK: d.topK})
}

// HistorySession replays a persisted conversation: the full page shell,
// pre-populated with its turns, with the composer wired to keep appending
// to the same session.
func (d *dependencies) HistorySession(c *gin.Context) {
	if d.history == nil {
		c.String(http.StatusNotFound, "chat history is disabled")
		return
	}

	sessionID := c.Param("id")
	turns, err := d.history.ListTurns(c.Request.Context(), sessionID)
	if err != nil {
		d.log.Warn("history list turns failed", zap.String("session_id", sessionID), zap.Error(err))
		c.String(http.StatusNotFound, "session not found")
		return
	}

	views := make([]chatapi.TurnView, len(turns))
	for i, t := range turns {
		views[i] = chatapi.HistoryTurnToView(t)
	}
	d.renderPage(c, pageView{DefaultTopK: d.topK, SessionID: sessionID, Turns: views})
}

func (d *dependencies) renderPage(c *gin.Context, data pageView) {
	d.render.HTML(c, http.StatusOK, "page", data)
}
