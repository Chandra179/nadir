package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// RequestLog logs one line per HTTP request: Info 2xx/3xx, Warn 4xx,
// Error 5xx (attaching the handler's c.Error(err) for 4xx/5xx). Bodies are
// never logged.
func (d *dependencies) RequestLog() gin.HandlerFunc {
	return func(c *gin.Context) {

		start := time.Now()
		c.Next()

		duration := time.Since(start)
		status := c.Writer.Status()

		fields := []zap.Field{
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", status),
			zap.Int64("duration_ms", duration.Milliseconds()),
		}

		if c.Request.URL.RawQuery != "" {
			fields = append(fields, zap.String("query", c.Request.URL.RawQuery))
		}

		switch {
		case status >= http.StatusInternalServerError:
			d.logger.Error("request completed", withLastError(c, fields)...)
		case status >= http.StatusBadRequest:
			d.logger.Warn("request completed", withLastError(c, fields)...)
		default:
			d.logger.Info("request completed", fields...)
		}
	}
}

// withLastError appends the last error recorded via c.Error(err), if any.
// Only called for 4xx/5xx responses.
func withLastError(c *gin.Context, fields []zap.Field) []zap.Field {
	if err := c.Errors.Last(); err != nil {
		return append(fields, zap.Error(err.Err))
	}
	return fields
}
