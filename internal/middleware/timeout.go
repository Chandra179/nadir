package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Timeout attaches a deadline to the request context so downstream Qdrant
// and Ollama calls return instead of hanging. POST /ingest is excluded: a
// full source.paths sweep is a legitimately long-running bulk operation.
func Timeout(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d <= 0 || (c.Request.Method == http.MethodPost && c.Request.URL.Path == "/ingest") {
			c.Next()
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
