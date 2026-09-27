package history

import (
	"nadir/internal/edge/http/respond"
	"net/http"
	"time"

	"log/slog"

	"nadir/internal/core/conversation/history"
	"nadir/internal/edge/http/contract"
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
func (h *Handlers) ListSessions(w http.ResponseWriter, r *http.Request) {
	view := SessionsResponse{Enabled: h.history != nil, Sessions: []SessionResponse{}}
	if h.history != nil {
		sessions, err := h.history.ListSessions(r.Context(), h.sessionPageSize)
		if err != nil {
			h.log.Warn("history list sessions failed", slog.Any("error", err))
		} else {
			view.Sessions = make([]SessionResponse, len(sessions))
			for i, session := range sessions {
				view.Sessions[i] = sessionResponse(session)
			}
		}
	}

	respond.JSON(w, http.StatusOK, view)
}

// GetSession returns one session and its ordered turns.
func (h *Handlers) GetSession(w http.ResponseWriter, r *http.Request) {
	if h.history == nil {
		respond.JSON(w, http.StatusNotFound, map[string]any{"error": "chat history is disabled"})
		return
	}
	sessionID := r.PathValue("id")
	session, err := h.history.GetSession(r.Context(), sessionID)
	if err != nil {
		respond.JSON(w, http.StatusNotFound, map[string]any{"error": "session not found"})
		return
	}
	turns, err := h.history.ListTurns(r.Context(), sessionID)
	if err != nil {
		h.log.Warn("history list turns failed", slog.String("session_id", sessionID), slog.Any("error", err))
		respond.JSON(w, http.StatusInternalServerError, map[string]any{"error": "load session failed"})
		return
	}
	response := SessionDetailResponse{Session: sessionResponse(session), Turns: make([]contract.TurnResponse, len(turns))}
	for i, turn := range turns {
		response.Turns[i] = contract.FromHistoryTurn(turn)
	}
	respond.JSON(w, http.StatusOK, response)
}

// DeleteSession permanently removes a chat session and all of its turns.
func (h *Handlers) DeleteSession(w http.ResponseWriter, r *http.Request) {
	if h.history == nil {
		respond.JSON(w, http.StatusNotFound, map[string]any{"error": "chat history is disabled"})
		return
	}

	sessionID := r.PathValue("id")
	if h.chat == nil {
		respond.JSON(w, http.StatusInternalServerError, map[string]any{"error": "chat lifecycle unavailable"})
		return
	}
	if err := h.chat.DeleteSession(r.Context(), sessionID); err != nil {
		h.log.Warn("history delete session failed", slog.String("session_id", sessionID), slog.Any("error", err))
		respond.JSON(w, http.StatusInternalServerError, map[string]any{"error": "delete failed"})
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// DeleteAllSessions permanently removes every chat session and turn.
// The document corpus is a separate concern and is intentionally untouched.
func (h *Handlers) DeleteAllSessions(w http.ResponseWriter, r *http.Request) {
	if h.history == nil {
		respond.JSON(w, http.StatusNotFound, map[string]any{"error": "chat history is disabled"})
		return
	}

	if h.chat == nil {
		respond.JSON(w, http.StatusInternalServerError, map[string]any{"error": "chat lifecycle unavailable"})
		return
	}
	if err := h.chat.DeleteAllSessions(r.Context()); err != nil {
		h.log.Warn("history delete all sessions failed", slog.Any("error", err))
		respond.JSON(w, http.StatusInternalServerError, map[string]any{"error": "delete all chats failed"})
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{"deleted": true})
}
