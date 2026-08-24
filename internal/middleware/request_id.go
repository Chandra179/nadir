package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/gin-gonic/gin"
)

type contextKey string

const requestIDKey contextKey = "requestID"

const headerKey = "X-Request-ID"

func generateRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// RequestID is a Gin middleware that reads X-Request-ID from the request
// header, reusing it if present, or generates a random one. The ID is
// stored in the request context and echoed in the response header.
func RequestID(c *gin.Context) {
	id := c.GetHeader(headerKey)
	if id == "" {
		id = generateRequestID()
	}
	c.Header(headerKey, id)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), requestIDKey, id))
	c.Next()
}

