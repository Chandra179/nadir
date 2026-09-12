package history

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"nadir/internal/api/render"
	domainchat "nadir/internal/chat"
	domainhistory "nadir/internal/history"
)

type fakeHistory struct {
	deleteAllCalls int
	err            error
}

func (f *fakeHistory) CreateSession(context.Context, string) (domainhistory.Session, error) {
	return domainhistory.Session{}, nil
}

func (f *fakeHistory) TruncateSession(context.Context, string, int) error { return nil }

func (f *fakeHistory) AppendTurn(context.Context, string, domainhistory.Turn, string) error {
	return nil
}

func (f *fakeHistory) ListSessions(context.Context, int) ([]domainhistory.Session, error) {
	return nil, nil
}

func (f *fakeHistory) GetSession(context.Context, string) (domainhistory.Session, error) {
	return domainhistory.Session{}, nil
}

func (f *fakeHistory) ListTurns(context.Context, string) ([]domainhistory.Turn, error) {
	return nil, nil
}

func (f *fakeHistory) DeleteSession(context.Context, string) error { return nil }

func (f *fakeHistory) DeleteAllSessions(context.Context) error {
	f.deleteAllCalls++
	return f.err
}

type fakeChat struct {
	deleteCalls    []string
	deleteAllCalls int
	err            error
}

func (f *fakeChat) StartTurn(context.Context, domainchat.Request) domainchat.Turn {
	return domainchat.Turn{}
}

func (f *fakeChat) Subscribe(context.Context, string, int64) (<-chan domainchat.TurnEvent, func(), bool) {
	return nil, nil, false
}

func (f *fakeChat) CancelTurn(string) bool { return false }

func (f *fakeChat) DeleteSession(_ context.Context, sessionID string) error {
	f.deleteCalls = append(f.deleteCalls, sessionID)
	return f.err
}

func (f *fakeChat) DeleteAllSessions(context.Context) error {
	f.deleteAllCalls++
	return f.err
}

func testContext(method, target string) (*gin.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, target, nil)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = req
	return c, recorder
}

func TestHistorySessionsDeleteAll(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeHistory{}
	chat := &fakeChat{}
	h := New(Config{History: fake, Chat: chat, Render: render.New(nil)})
	c, recorder := testContext("DELETE", "/history/sessions")

	h.HistorySessionsDeleteAll(c)

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if chat.deleteAllCalls != 1 {
		t.Fatalf("chat DeleteAllSessions calls = %d, want 1", chat.deleteAllCalls)
	}
	if fake.deleteAllCalls != 0 {
		t.Fatalf("handler must not bypass chat lifecycle, history DeleteAllSessions calls = %d", fake.deleteAllCalls)
	}
}

func TestHistorySessionsDeleteAllReturnsError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeHistory{}
	chat := &fakeChat{err: context.Canceled}
	h := New(Config{History: fake, Chat: chat, Render: render.New(nil)})
	c, recorder := testContext("DELETE", "/history/sessions")

	h.HistorySessionsDeleteAll(c)

	if recorder.Code != 500 {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}

func TestHistorySessionDeleteUsesChatLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeHistory{}
	chat := &fakeChat{}
	h := New(Config{History: fake, Chat: chat, Render: render.New(nil)})
	c, recorder := testContext("DELETE", "/history/sessions/session-1")
	c.Params = gin.Params{{Key: "id", Value: "session-1"}}

	h.HistorySessionDelete(c)

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if len(chat.deleteCalls) != 1 || chat.deleteCalls[0] != "session-1" {
		t.Fatalf("chat DeleteSession calls = %v, want [session-1]", chat.deleteCalls)
	}
}
