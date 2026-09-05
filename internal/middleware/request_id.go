package middleware

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/gin-gonic/gin"
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
	c.Next()
}
