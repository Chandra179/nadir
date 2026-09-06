package chat

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"nadir/internal/chat"
)

// RetrievalAnswer subscribes to a turn's event log and adapts it to
// Server-Sent Events: one `token` event per answer chunk, a terminal
// `done`/`generror`. The SSE `id:` field carries the log cursor, so a
// reconnecting browser resumes from its Last-Event-ID instead of missing
// or duplicating events. Generation is not touched here — the chat service
// owns it; this endpoint only observes.
func (h *Handlers) RetrievalAnswer(c *gin.Context) {
	since := int64(0)
	if v, err := strconv.ParseInt(c.GetHeader("Last-Event-ID"), 10, 64); err == nil && v > 0 {
		since = v
	}

	events, cancel, ok := h.chat.Subscribe(c.Request.Context(), c.Param("id"), since)
	if !ok {
		c.String(http.StatusNotFound, "no such turn event stream")
		return
	}
	defer cancel()

	w := c.Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	// The status goes out with the first event: a finished stream whose log
	// the client has already consumed (e.g. a reconnect carrying
	// Last-Event-ID) delivers zero events, and 204 tells the EventSource to
	// stop reconnecting instead of looping on empty 200s.
	delivered := 0
	for ev := range events {
		if delivered == 0 {
			w.WriteHeader(http.StatusOK)
		}
		delivered++
		switch ev.Kind {
		case chat.EventToken:
			_ = writeSSEEvent(w, "token", ev.Text, ev.Seq)
		case chat.EventError:
			_ = writeSSEEvent(w, "generror", ev.Text, ev.Seq)
		case chat.EventDone:
			_ = writeSSEEvent(w, "done", "1", ev.Seq)
		}
		w.Flush()
	}
	if delivered == 0 {
		w.WriteHeader(http.StatusNoContent)
	}
}

// RetrievalTurnCancel aborts a turn's in-flight generation. The supervisor
// keeps the answer generated so far, emits its terminal event and persists
// the partial answer; unknown turn ids are 404.
func (h *Handlers) RetrievalTurnCancel(c *gin.Context) {
	if !h.chat.CancelTurn(c.Param("id")) {
		c.Status(http.StatusNotFound)
		return
	}
	c.Status(http.StatusNoContent)
}

// writeSSEEvent writes one named, sequenced SSE event. The payload stays
// raw — the browser inserts it with textContent, so HTML escaping would
// only double-escape. Newlines are normalized and split across data: lines
// (the SSE client joins them back with \n) to keep the framing intact.
func writeSSEEvent(w gin.ResponseWriter, event, text string, seq int64) error {
	if _, err := w.WriteString("id: " + strconv.FormatInt(seq, 10) + "\n"); err != nil {
		return err
	}
	if _, err := w.WriteString("event: " + event + "\n"); err != nil {
		return err
	}
	normalized := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text)
	for _, line := range strings.Split(normalized, "\n") {
		if _, err := w.WriteString("data: " + line + "\n"); err != nil {
			return err
		}
	}
	_, err := w.WriteString("\n")
	return err
}
