package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	domainchat "nadir/internal/conversation/chat"
)

type fakeChat struct {
	turn     domainchat.Turn
	events   []domainchat.TurnEvent
	ok       bool
	gotSince int64
	cancelOK bool

	mu       sync.Mutex
	started  []domainchat.Request
	canceled []string
}

func (f *fakeChat) StartTurn(_ context.Context, req domainchat.Request) domainchat.Turn {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, req)
	return f.turn
}

func (f *fakeChat) Subscribe(_ context.Context, _ string, since int64) (<-chan domainchat.TurnEvent, func(), bool) {
	if !f.ok {
		return nil, nil, false
	}
	f.mu.Lock()
	f.gotSince = since
	f.mu.Unlock()
	ch := make(chan domainchat.TurnEvent, len(f.events))
	for _, ev := range f.events {
		ch <- ev
	}
	close(ch)
	return ch, func() {}, true
}

func (f *fakeChat) CancelTurn(turnID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.canceled = append(f.canceled, turnID)
	return f.cancelOK
}

func (f *fakeChat) DeleteSession(context.Context, string) error { return nil }
func (f *fakeChat) DeleteAllSessions(context.Context) error     { return nil }

func turnTestServer(t *testing.T, fc *fakeChat) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := New(Config{Chat: fc, TopK: 8})
	engine := gin.New()
	engine.POST("/api/v1/turns", h.StartTurn)
	engine.GET("/api/v1/turns/:id/events", h.StreamTurn)
	engine.POST("/api/v1/turns/:id/cancel", h.CancelTurn)
	return engine
}

func TestStreamTurnAdaptsEventsToSSE(t *testing.T) {
	fc := &fakeChat{ok: true, events: []domainchat.TurnEvent{
		{Seq: 1, Kind: domainchat.EventToken, Text: "Hello <world>."},
		{Seq: 2, Kind: domainchat.EventToken, Text: "Second\nline"},
		{Seq: 3, Kind: domainchat.EventError, Text: "boom"},
		{Seq: 4, Kind: domainchat.EventDone, Text: "1"},
	}}
	w := httptest.NewRecorder()
	turnTestServer(t, fc).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/turns/t1/events", nil))

	body := w.Body.String()
	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q", w.Header().Get("Content-Type"))
	}
	if !strings.Contains(body, "id: 1\nevent: token\ndata: Hello <world>.\n\n") ||
		!strings.Contains(body, "data: Second\ndata: line\n\n") ||
		!strings.Contains(body, "event: generror") || !strings.Contains(body, "event: done") {
		t.Fatalf("unexpected SSE response:\n%s", body)
	}
}

func TestStreamTurnForwardsLastEventIDCursor(t *testing.T) {
	fc := &fakeChat{ok: true}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/turns/t1/events", nil)
	req.Header.Set("Last-Event-ID", "3")
	turnTestServer(t, fc).ServeHTTP(httptest.NewRecorder(), req)
	if fc.gotSince != 3 {
		t.Fatalf("Last-Event-ID = %d, want 3", fc.gotSince)
	}
}

func TestStartTurnReturnsJSONAndParsesEdit(t *testing.T) {
	fc := &fakeChat{turn: domainchat.Turn{ID: "t9", SessionID: "s1", Streaming: true, Generate: true}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/turns", strings.NewReader(`{"query":"edited","generate":true,"session_id":"s1","edit":true,"edit_sequence":2,"filter":{"file_path":"math.md"}}`))
	req.Header.Set("Content-Type", "application/json")
	turnTestServer(t, fc).ServeHTTP(w, req)

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"stream_url":"/api/v1/turns/t9/events"`) {
		t.Fatalf("response = %d/%s", w.Code, w.Body.String())
	}
	if len(fc.started) != 1 || !fc.started[0].Edit || fc.started[0].EditSequence != 2 || fc.started[0].SessionID != "s1" {
		t.Fatalf("request = %+v", fc.started)
	}
	if fc.started[0].Filter == nil || fc.started[0].Filter.FilePath != "math.md" {
		t.Fatalf("filter = %+v", fc.started[0].Filter)
	}
}

func TestStartTurnRejectsMalformedJSON(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/turns", strings.NewReader("not-json"))
	turnTestServer(t, &fakeChat{}).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestCancelTurn(t *testing.T) {
	fc := &fakeChat{cancelOK: true}
	w := httptest.NewRecorder()
	turnTestServer(t, fc).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/turns/t1/cancel", nil))
	if w.Code != http.StatusNoContent || len(fc.canceled) != 1 || fc.canceled[0] != "t1" {
		t.Fatalf("cancel response = %d calls=%v", w.Code, fc.canceled)
	}
}
