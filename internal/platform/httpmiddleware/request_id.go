package middleware

import (
	"github.com/gin-gonic/gin"
	"nadir/internal/platform/observability"
)

const headerKey = "X-Request-ID"

// RequestID reads X-Request-ID from the request header, reusing it if
// present, or generates a random one, and echoes it in the response header.
func RequestID(c *gin.Context) {
	id := c.GetHeader(headerKey)
	if id == "" {
		id = observability.NewID("")
	}
	c.Header(headerKey, id)
	requestContext := observability.WithRequestID(c.Request.Context(), id)
	c.Request = c.Request.WithContext(requestContext)
	c.Header("X-Trace-ID", observability.TraceID(requestContext))
	c.Next()
}
