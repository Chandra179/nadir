package history

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
	"nadir/mocks"
)

func testContext(method, target string) (*gin.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, target, nil)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = req
	return c, recorder
}

func TestDeleteAllSessions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	history := &mocks.MockReader{}
	chat := &mocks.MockChat{}
	chat.EXPECT().DeleteAllSessions(mock.Anything).Return(nil)
	h := NewDependencies(DependenciesConfig{History: history, Chat: chat})
	c, recorder := testContext("DELETE", "/api/v1/sessions")

	h.DeleteAllSessions(c)

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	chat.AssertNumberOfCalls(t, "DeleteAllSessions", 1)
}

func TestDeleteAllSessionsReturnsError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	history := &mocks.MockReader{}
	chat := &mocks.MockChat{}
	chat.EXPECT().DeleteAllSessions(mock.Anything).Return(context.Canceled)
	h := NewDependencies(DependenciesConfig{History: history, Chat: chat})
	c, recorder := testContext("DELETE", "/api/v1/sessions")

	h.DeleteAllSessions(c)

	if recorder.Code != 500 {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}

func TestDeleteSessionUsesChatLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	history := &mocks.MockReader{}
	chat := &mocks.MockChat{}
	chat.EXPECT().DeleteSession(mock.Anything, "session-1").Return(nil)
	h := NewDependencies(DependenciesConfig{History: history, Chat: chat})
	c, recorder := testContext("DELETE", "/api/v1/sessions/session-1")
	c.Params = gin.Params{{Key: "id", Value: "session-1"}}

	h.DeleteSession(c)

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	chat.AssertNumberOfCalls(t, "DeleteSession", 1)
}
