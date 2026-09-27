package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestIDReusesOrGeneratesHeader(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, supplied := range []string{"client-id", ""} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if supplied != "" {
			req.Header.Set(headerKey, supplied)
		}
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		got := resp.Header().Get(headerKey)
		if got == "" || supplied != "" && got != supplied {
			t.Fatalf("response request id = %q, supplied %q", got, supplied)
		}
	}
}

func TestTimeoutAttachesDeadlineAndExemptsStreams(t *testing.T) {
	var deadlineSeen, streamDeadlineSeen bool
	handler := Timeout(50 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasDeadline := r.Context().Deadline()
		if r.URL.Path == "/normal" {
			deadlineSeen = hasDeadline
		} else {
			streamDeadlineSeen = hasDeadline
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/normal", nil))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/turns/id/events", nil))
	if !deadlineSeen || streamDeadlineSeen {
		t.Fatalf("timeout middleware deadline normal=%v stream=%v, want true/false", deadlineSeen, streamDeadlineSeen)
	}
}
