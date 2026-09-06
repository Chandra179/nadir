package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"nadir/internal/api/internal/render"
	"nadir/internal/chat"
)

// fakeChat backs the SSE adapter tests: StartTurn returns a canned turn,
// Subscribe replays canned events and records the requested cursor.
type fakeChat struct {
	turn     chat.Turn
	events   []chat.TurnEvent
	ok       bool
	gotSince int64
	cancelOK bool

	mu       sync.Mutex
	started  []chat.Request
	canceled []string
}

func (f *fakeChat) StartTurn(ctx context.Context, req chat.Request) chat.Turn {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, req)
	return f.turn
}

func (f *fakeChat) Subscribe(ctx context.Context, turnID string, since int64) (<-chan chat.TurnEvent, func(), bool) {
	if !f.ok {
		return nil, nil, false
	}
	f.mu.Lock()
	f.gotSince = since
	f.mu.Unlock()
	ch := make(chan chat.TurnEvent, len(f.events))
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

func turnTestServer(t *testing.T, fc *fakeChat) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := New(Config{Chat: fc, TopK: 8, Render: render.New(nil)})
	engine := gin.New()
	engine.POST("/retrieval/search", h.RetrievalSearch)
	engine.GET("/retrieval/turns/:id/events", h.RetrievalAnswer)
	engine.POST("/retrieval/turns/:id/cancel", h.RetrievalTurnCancel)
	return engine
}

func TestRetrievalAnswerAdaptsEventsToSSE(t *testing.T) {
	fc := &fakeChat{
		ok: true,
		turn: chat.Turn{
			ID: "t1", SessionID: "s1", Streaming: true,
			RewrittenQuery: "rewritten?",
		},
		events: []chat.TurnEvent{
			{Seq: 1, Kind: chat.EventToken, Text: "Hello <world>."},
			{Seq: 2, Kind: chat.EventToken, Text: "Second\nline"},
			{Seq: 3, Kind: chat.EventError, Text: "boom"},
			{Seq: 4, Kind: chat.EventDone, Text: "1"},
		},
	}
	engine := turnTestServer(t, fc)

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/retrieval/turns/t1/events", nil))

	body := w.Body.String()
	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q", w.Header().Get("Content-Type"))
	}
	// The browser inserts token text via textContent, so the payload must
	// stay raw — HTML escaping here would render as literal entities.
	if !strings.Contains(body, "id: 1\nevent: token\ndata: Hello <world>.\n\n") {
		t.Fatalf("token events must be sequenced and unescaped, got:\n%s", body)
	}
	if !strings.Contains(body, "data: Second\ndata: line\n\n") {
		t.Fatalf("token newlines must be split across data lines, got:\n%s", body)
	}
	if !strings.Contains(body, "event: generror") || !strings.Contains(body, "event: done") {
		t.Fatalf("error and done events must be forwarded, got:\n%s", body)
	}
}

func TestRetrievalAnswerForwardsLastEventIDCursor(t *testing.T) {
	fc := &fakeChat{ok: true}
	engine := turnTestServer(t, fc)

	req := httptest.NewRequest(http.MethodGet, "/retrieval/turns/t1/events", nil)
	req.Header.Set("Last-Event-ID", "3")
	engine.ServeHTTP(httptest.NewRecorder(), req)

	if fc.gotSince != 3 {
		t.Fatalf("Last-Event-ID must become the replay cursor, got %d", fc.gotSince)
	}
}

func TestRetrievalAnswerExhaustedStreamIs204(t *testing.T) {
	// A finished stream the client has fully consumed (a reconnect carrying
	// Last-Event-ID at the end) must not loop the client on empty 200s.
	fc := &fakeChat{ok: true} // Subscribe closes an empty channel
	engine := turnTestServer(t, fc)

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/retrieval/turns/t1/events", nil))

	if w.Code != http.StatusNoContent {
		t.Fatalf("exhausted stream must answer 204 so the client stops reconnecting, got %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("204 must carry no body, got %q", w.Body.String())
	}
}

func TestRetrievalAnswerUnknownTurnIs404(t *testing.T) {
	fc := &fakeChat{ok: false}
	engine := turnTestServer(t, fc)

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/retrieval/turns/nope/events", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("unsubscribable turn must 404, got %d", w.Code)
	}
}

func TestRetrievalTurnCancel(t *testing.T) {
	fc := &fakeChat{cancelOK: true}
	engine := turnTestServer(t, fc)

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/retrieval/turns/t1/cancel", nil))

	if w.Code != http.StatusNoContent {
		t.Fatalf("cancel of a known turn must be 204, got %d", w.Code)
	}
	if len(fc.canceled) != 1 || fc.canceled[0] != "t1" {
		t.Fatalf("cancel must reach the chat service with the turn id, got %v", fc.canceled)
	}

	fc2 := &fakeChat{cancelOK: false}
	engine2 := turnTestServer(t, fc2)
	w2 := httptest.NewRecorder()
	engine2.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/retrieval/turns/ghost/cancel", nil))

	if w2.Code != http.StatusNotFound {
		t.Fatalf("cancel of an unknown turn must 404, got %d", w2.Code)
	}
}

func TestRetrievalSearchRendersStreamURL(t *testing.T) {
	fc := &fakeChat{turn: chat.Turn{ID: "t9", SessionID: "s1", Streaming: true, Generate: true}}
	engine := turnTestServer(t, fc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/retrieval/search", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.PostForm = map[string][]string{"query": {"q"}, "generate": {"on"}, "session_id": {"s1"}}
	engine.ServeHTTP(w, req)

	if w.Header().Get("X-Nadir-Session-Id") != "s1" {
		t.Fatalf("session header must be echoed for follow-ups, got %q", w.Header().Get("X-Nadir-Session-Id"))
	}
	if !strings.Contains(w.Body.String(), `data-stream-url="/retrieval/turns/t9/events"`) ||
		!strings.Contains(w.Body.String(), `data-turn-id="t9"`) {
		t.Fatalf("streaming turn fragment must expose the event stream and turn id, got:\n%s", w.Body.String())
	}
}
