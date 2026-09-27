package chat

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"nadir/internal/core/conversation/chat"
)

// StreamTurn subscribes to a turn's event log and adapts it to
// Server-Sent Events: one `token` event per answer chunk, a terminal
// `done`/`generror`. The SSE `id:` field carries the log cursor, so a
// reconnecting browser resumes from its Last-Event-ID instead of missing
// or duplicating events. Generation is not touched here — the chat service
// owns it; this endpoint only observes.
func (h *Handlers) StreamTurn(w http.ResponseWriter, r *http.Request) {
	since := int64(0)
	if v, err := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64); err == nil && v > 0 {
		since = v
	}

	events, cancel, ok := h.chat.Subscribe(r.Context(), r.PathValue("id"), since)
	if !ok {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "no such turn event stream")
		return
	}
	defer cancel()

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
		case chat.EventReplayGap:
			_ = writeSSEEvent(w, "resync", ev.Text, ev.Seq)
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
	if delivered == 0 {
		w.WriteHeader(http.StatusNoContent)
	}
}

// CancelTurn aborts a turn's in-flight generation. The supervisor
// keeps the answer generated so far, emits its terminal event and persists
// the partial answer; unknown turn ids are 404.
func (h *Handlers) CancelTurn(w http.ResponseWriter, r *http.Request) {
	if !h.chat.CancelTurn(r.PathValue("id")) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeSSEEvent writes one named, sequenced SSE event. The payload stays
// raw — the browser inserts it with textContent, so HTML escaping would
// only double-escape. Newlines are normalized and split across data: lines
// (the SSE client joins them back with \n) to keep the framing intact.
func writeSSEEvent(w io.Writer, event, text string, seq int64) error {
	if _, err := io.WriteString(w, "id: "+strconv.FormatInt(seq, 10)+"\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "event: "+event+"\n"); err != nil {
		return err
	}
	normalized := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text)
	for line := range strings.SplitSeq(normalized, "\n") {
		if _, err := io.WriteString(w, "data: "+line+"\n"); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "\n")
	return err
}
