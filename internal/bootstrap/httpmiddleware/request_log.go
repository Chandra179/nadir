package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"nadir/internal/core/observability"
)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *statusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}
func (w *statusWriter) Flush()                      { _ = http.NewResponseController(w.ResponseWriter).Flush() }
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// RequestLog records one structured line per request.
func (d *dependencies) RequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		status := sw.status
		if status == 0 {
			status = http.StatusOK
		}
		if d.logger == nil {
			return
		}
		path := r.Pattern
		if path == "" {
			path = "<unmatched>"
		}
		fields := []slog.Attr{
			slog.String("method", r.Method), slog.String("path", path),
			slog.Int("status", status), slog.Int64("duration_ms", time.Since(started).Milliseconds()),
		}
		if id := observability.RequestID(r.Context()); id != "" {
			fields = append(fields, slog.String("request_id", id))
		}
		if id := observability.TraceID(r.Context()); id != "" {
			fields = append(fields, slog.String("trace_id", id))
		}
		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelError
		} else if status >= 400 {
			level = slog.LevelWarn
		}
		d.logger.LogAttrs(r.Context(), level, "request completed", fields...)
	})
}

// Recovery keeps an application panic from terminating the process.
func Recovery(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					if log != nil {
						log.LogAttrs(context.Background(), slog.LevelError, "http panic", slog.Any("error", err))
					}
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
