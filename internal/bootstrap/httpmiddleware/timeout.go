package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// Timeout bounds ordinary requests; bulk indexing and SSE outlive this budget.
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if d <= 0 || (r.Method == http.MethodPost && r.URL.Path == "/api/v1/documents") || strings.HasPrefix(r.URL.Path, "/api/v1/turns/") {
				next.ServeHTTP(w, r)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
