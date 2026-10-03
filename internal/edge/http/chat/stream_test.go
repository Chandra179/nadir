package chat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"

	domainchat "nadir/internal/core/conversation/chat"
	"nadir/internal/core/retrieval/search"
	"nadir/internal/testmocks"
)

func TestStartTurnProviderFailureReturnsSafeJSON(t *testing.T) {
	const private = "private-provider-json-canary"
	retriever := &mocks.MockRetriever{}
	retriever.EXPECT().Query(mock.Anything, mock.Anything).Return(search.Result{}, errors.New("provider credential="+private))
	chat := domainchat.NewDependencies(domainchat.DependenciesConfig{Searcher: retriever})
	w := httptest.NewRecorder()
	turnTestServer(t, chat).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/turns", strings.NewReader(`{"query":"A question"}`)))
	var body struct {
		Error string `json:"error"`
		Query string `json:"query"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || !strings.Contains(body.Error, "Search failed") || strings.Contains(w.Body.String(), private) || body.Query != "A question" {
		t.Fatalf("unsafe or incompatible failure response: %d %s", w.Code, w.Body.String())
	}
}

func turnTestServer(t *testing.T, chat domainchat.Chat) http.Handler {
	t.Helper()
	h := NewDependencies(DependenciesConfig{Chat: chat, TopK: 8})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/turns", h.StartTurn)
	mux.HandleFunc("GET /api/v1/turns/{id}/events", h.StreamTurn)
	mux.HandleFunc("POST /api/v1/turns/{id}/cancel", h.CancelTurn)
	return mux
}

func TestStreamTurnAdaptsEventsToSSE(t *testing.T) {
	fc := &mocks.MockChat{}
	events := make(chan domainchat.TurnEvent, 4)
	for _, ev := range []domainchat.TurnEvent{
		{Seq: 1, Kind: domainchat.EventToken, Text: "Hello <world>."},
		{Seq: 2, Kind: domainchat.EventToken, Text: "Second\nline"},
		{Seq: 3, Kind: domainchat.EventError, Text: "boom"},
		{Seq: 4, Kind: domainchat.EventDone, Text: "1"},
	} {
		events <- ev
	}
	close(events)
	fc.EXPECT().Subscribe(mock.Anything, "t1", int64(0)).Return((<-chan domainchat.TurnEvent)(events), func() {}, true)
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
	fc := &mocks.MockChat{}
	events := make(chan domainchat.TurnEvent)
	close(events)
	fc.EXPECT().Subscribe(mock.Anything, "t1", int64(3)).Return((<-chan domainchat.TurnEvent)(events), func() {}, true)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/turns/t1/events", nil)
	req.Header.Set("Last-Event-ID", "3")
	turnTestServer(t, fc).ServeHTTP(httptest.NewRecorder(), req)
}

func TestStartTurnReturnsJSONAndParsesEdit(t *testing.T) {
	fc := &mocks.MockChat{}
	var got domainchat.Request
	fc.EXPECT().StartTurn(mock.Anything, mock.Anything).Run(func(_ context.Context, req domainchat.Request) {
		got = req
	}).Return(domainchat.Turn{ID: "t9", SessionID: "s1", Streaming: true, Generate: true})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/turns", strings.NewReader(`{"query":"edited","generate":true,"session_id":"s1","edit":true,"edit_sequence":2,"filter":{"file_path":"math.md"}}`))
	req.Header.Set("Content-Type", "application/json")
	turnTestServer(t, fc).ServeHTTP(w, req)

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"stream_url":"/api/v1/turns/t9/events"`) {
		t.Fatalf("response = %d/%s", w.Code, w.Body.String())
	}
	if !got.Edit || got.EditSequence != 2 || got.SessionID != "s1" {
		t.Fatalf("request = %+v", got)
	}
	if got.Filter == nil || got.Filter.FilePath != "math.md" {
		t.Fatalf("filter = %+v", got.Filter)
	}
}

func TestStartTurnRejectsMalformedJSON(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/turns", strings.NewReader("not-json"))
	turnTestServer(t, &mocks.MockChat{}).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestCancelTurn(t *testing.T) {
	fc := &mocks.MockChat{}
	fc.EXPECT().CancelTurn("t1").Return(true)
	w := httptest.NewRecorder()
	turnTestServer(t, fc).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/turns/t1/cancel", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("cancel response = %d", w.Code)
	}
}
