package history

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nadir/internal/conversation/history"
	"nadir/internal/transport/http/contract"
)

// SessionResponse is the public JSON representation of a chat session.
type SessionResponse struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	TurnCount int    `json:"turn_count"`
}

// SessionsResponse is the sidebar session-list response.
type SessionsResponse struct {
	Enabled  bool              `json:"enabled"`
	Sessions []SessionResponse `json:"sessions"`
}

// SessionDetailResponse combines one session with its persisted turns.
type SessionDetailResponse struct {
	Session SessionResponse         `json:"session"`
	Turns   []contract.TurnResponse `json:"turns"`
}

func sessionResponse(s history.Session) SessionResponse {
	return SessionResponse{
		ID: s.ID, Title: s.Title, CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: s.UpdatedAt.UTC().Format(time.RFC3339Nano), TurnCount: s.TurnCount,
	}
}

// ListSessions returns the sidebar's ordered session list.
func (h *Handlers) ListSessions(c *gin.Context) {
	view := SessionsResponse{Enabled: h.history != nil, Sessions: []SessionResponse{}}
	if h.history != nil {
		sessions, err := h.history.ListSessions(c.Request.Context(), 50)
		if err != nil {
			h.log.Warn("history list sessions failed", zap.Error(err))
		} else {
			view.Sessions = make([]SessionResponse, len(sessions))
			for i, session := range sessions {
				view.Sessions[i] = sessionResponse(session)
			}
		}
	}

	c.JSON(http.StatusOK, view)
}

// GetSession returns one session and its ordered turns.
func (h *Handlers) GetSession(c *gin.Context) {
	if h.history == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat history is disabled"})
		return
	}
	sessionID := c.Param("id")
	session, err := h.history.GetSession(c.Request.Context(), sessionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	turns, err := h.history.ListTurns(c.Request.Context(), sessionID)
	if err != nil {
		h.log.Warn("history list turns failed", zap.String("session_id", sessionID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load session failed"})
		return
	}
	response := SessionDetailResponse{Session: sessionResponse(session), Turns: make([]contract.TurnResponse, len(turns))}
	for i, turn := range turns {
		response.Turns[i] = contract.FromHistoryTurn(turn)
	}
	c.JSON(http.StatusOK, response)
}

// DeleteSession permanently removes a chat session and all of its turns.
func (h *Handlers) DeleteSession(c *gin.Context) {
	if h.history == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat history is disabled"})
		return
	}

	sessionID := c.Param("id")
	if h.chat == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "chat lifecycle unavailable"})
		return
	}
	if err := h.chat.DeleteSession(c.Request.Context(), sessionID); err != nil {
		h.log.Warn("history delete session failed", zap.String("session_id", sessionID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// DeleteAllSessions permanently removes every chat session and turn.
// The document corpus is a separate concern and is intentionally untouched.
func (h *Handlers) DeleteAllSessions(c *gin.Context) {
	if h.history == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat history is disabled"})
		return
	}

	if h.chat == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "chat lifecycle unavailable"})
		return
	}
	if err := h.chat.DeleteAllSessions(c.Request.Context()); err != nil {
		h.log.Warn("history delete all sessions failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete all chats failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}
