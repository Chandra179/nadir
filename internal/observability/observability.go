// Package observability centralizes the small amount of structured stage
// logging shared by domain Adapters and use-cases. It deliberately records
// bounded outcome labels rather than provider error strings as dimensions.
package observability

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Stage records one bounded stage outcome and duration. Successful and
// non-error outcomes are Debug-level so normal production logs stay quiet;
// failures are Warn-level with a bounded error label.
func Stage(log *zap.Logger, stage, outcome string, started time.Time, err error, fields ...zap.Field) {
	if log == nil {
		return
	}
	fields = append(fields,
		zap.String("stage", stage),
		zap.String("outcome", outcome),
		zap.Int64("duration_ms", time.Since(started).Milliseconds()),
	)
	if err != nil {
		fields = append(fields, zap.String("error_label", ErrorLabel(err)))
		log.Warn("stage completed", fields...)
		return
	}
	log.Debug("stage completed", fields...)
}

// ErrorLabel maps arbitrary provider errors to a bounded operational label.
// Error text remains available at the call site when needed, but is not
// emitted as a metric/log dimension by this helper.
func ErrorLabel(err error) string {
	if err == nil {
		return "none"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "generation"):
		return "generation_error"
	case strings.Contains(message, "status "):
		return "remote_status"
	case strings.Contains(message, "decode"), strings.Contains(message, "unmarshal"), strings.Contains(message, "malformed"):
		return "malformed_response"
	case strings.Contains(message, "missing"), strings.Contains(message, "mismatch"), strings.Contains(message, "empty"):
		return "response_shape"
	case strings.Contains(message, "qdrant"), strings.Contains(message, "ollama"), strings.Contains(message, "sidecar"):
		return "dependency_error"
	default:
		return "unknown"
	}
}
