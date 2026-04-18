package middleware

import (
	"context"
	"net/http"
	"time"
)

type TimeoutConfig struct {
	Duration time.Duration
}

func Timeout(cfg TimeoutConfig) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), cfg.Duration)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
