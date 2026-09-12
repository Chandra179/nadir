package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Timeout attaches a deadline to the request context so downstream Qdrant
// and Ollama calls return instead of hanging. POST /api/v1/documents is excluded: a
// full source.paths sweep is a legitimately long-running bulk operation.
// Turn event streams (/api/v1/turns/) are excluded too: an SSE response
// is legitimately longer than the query budget.
func Timeout(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d <= 0 ||
			(c.Request.Method == http.MethodPost && c.Request.URL.Path == "/api/v1/documents") ||
			strings.HasPrefix(c.Request.URL.Path, "/api/v1/turns/") {
			c.Next()
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
