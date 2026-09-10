package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRequestIDReusesOrGeneratesHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestID)
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for _, supplied := range []string{"client-id", ""} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if supplied != "" {
			req.Header.Set(headerKey, supplied)
		}
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		got := resp.Header().Get(headerKey)
		if got == "" || supplied != "" && got != supplied {
			t.Fatalf("response request id = %q, supplied %q", got, supplied)
		}
	}
}

func TestTimeoutAttachesDeadlineAndExemptsStreams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var deadlineSeen, streamDeadlineSeen bool
	router := gin.New()
	router.Use(Timeout(50 * time.Millisecond))
	router.GET("/normal", func(c *gin.Context) {
		_, deadlineSeen = c.Request.Context().Deadline()
		c.Status(http.StatusNoContent)
	})
	router.GET("/retrieval/turns/id/events", func(c *gin.Context) {
		_, streamDeadlineSeen = c.Request.Context().Deadline()
		c.Status(http.StatusNoContent)
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/normal", nil))
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/retrieval/turns/id/events", nil))
	if !deadlineSeen || streamDeadlineSeen {
		t.Fatalf("timeout middleware deadline normal=%v stream=%v, want true/false", deadlineSeen, streamDeadlineSeen)
	}
}
