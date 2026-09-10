package history

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"nadir/internal/api/render"
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
	h := New(Config{History: fake, Render: render.New(nil)})
	c, recorder := testContext("DELETE", "/history/sessions")

	h.HistorySessionsDeleteAll(c)

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if fake.deleteAllCalls != 1 {
		t.Fatalf("DeleteAllSessions calls = %d, want 1", fake.deleteAllCalls)
	}
}

func TestHistorySessionsDeleteAllReturnsError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeHistory{err: context.Canceled}
	h := New(Config{History: fake, Render: render.New(nil)})
	c, recorder := testContext("DELETE", "/history/sessions")

	h.HistorySessionsDeleteAll(c)

	if recorder.Code != 500 {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}
