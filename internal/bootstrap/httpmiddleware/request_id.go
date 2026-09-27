package middleware

import (
	"nadir/internal/core/observability"
	"net/http"
)

const headerKey = "X-Request-ID"

// RequestID attaches and echoes a correlation ID.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(headerKey)
		if id == "" {
			id = observability.NewID("")
		}
		ctx := observability.WithRequestID(r.Context(), id)
		w.Header().Set(headerKey, id)
		w.Header().Set("X-Trace-ID", observability.TraceID(ctx))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
