package history

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/mock"
	conversationhistory "nadir/internal/core/conversation/history"
	"nadir/internal/testmocks"
)

func testContext(method, target string) (*http.Request, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, target, nil)
	recorder := httptest.NewRecorder()
	return req, recorder
}

func TestDeleteAllSessions(t *testing.T) {
	history := &mocks.MockReader{}
	chat := &mocks.MockChat{}
	chat.EXPECT().DeleteAllSessions(mock.Anything).Return(nil)
	h := NewDependencies(DependenciesConfig{History: history, Chat: chat})
	req, recorder := testContext("DELETE", "/api/v1/sessions")

	h.DeleteAllSessions(recorder, req)

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	chat.AssertNumberOfCalls(t, "DeleteAllSessions", 1)
}

func TestDeleteAllSessionsReturnsError(t *testing.T) {
	history := &mocks.MockReader{}
	chat := &mocks.MockChat{}
	chat.EXPECT().DeleteAllSessions(mock.Anything).Return(context.Canceled)
	h := NewDependencies(DependenciesConfig{History: history, Chat: chat})
	req, recorder := testContext("DELETE", "/api/v1/sessions")

	h.DeleteAllSessions(recorder, req)

	if recorder.Code != 500 {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}

func TestDeleteSessionUsesChatLifecycle(t *testing.T) {
	history := &mocks.MockReader{}
	chat := &mocks.MockChat{}
	chat.EXPECT().DeleteSession(mock.Anything, "session-1").Return(nil)
	h := NewDependencies(DependenciesConfig{History: history, Chat: chat})
	req, recorder := testContext("DELETE", "/api/v1/sessions/session-1")
	req.SetPathValue("id", "session-1")

	h.DeleteSession(recorder, req)

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	chat.AssertNumberOfCalls(t, "DeleteSession", 1)
}

func TestListSessionsUsesConfiguredPageSize(t *testing.T) {
	history := &mocks.MockReader{}
	chat := &mocks.MockChat{}
	history.EXPECT().ListSessions(mock.Anything, 7).Return([]conversationhistory.Session{}, nil)
	h := NewDependencies(DependenciesConfig{History: history, Chat: chat, SessionPageSize: 7})
	req, recorder := testContext("GET", "/api/v1/sessions")

	h.ListSessions(recorder, req)

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	history.AssertNumberOfCalls(t, "ListSessions", 1)
}
