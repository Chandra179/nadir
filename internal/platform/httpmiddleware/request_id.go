package middleware

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/gin-gonic/gin"
	"nadir/internal/platform/observability"
)

const headerKey = "X-Request-ID"

func generateRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// RequestID reads X-Request-ID from the request header, reusing it if
// present, or generates a random one, and echoes it in the response header.
func RequestID(c *gin.Context) {
	id := c.GetHeader(headerKey)
	if id == "" {
		id = generateRequestID()
	}
	c.Header(headerKey, id)
	requestContext := observability.WithRequestID(c.Request.Context(), id)
	c.Request = c.Request.WithContext(requestContext)
	c.Header("X-Trace-ID", observability.TraceID(requestContext))
	c.Next()
}
