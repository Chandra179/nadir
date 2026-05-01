package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type contextKey string

const requestIDKey contextKey = "requestID"

const headerKey = "X-Request-ID"
const grpcMetaKey = "x-request-id" // gRPC metadata keys must be lowercase

func generateRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// storeRequestID stores id into ctx and returns the updated context.
func storeRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// GetRequestID retrieves the request ID from ctx. Returns "" if not set.
func GetRequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// RequestID is an HTTP middleware. It reads X-Request-ID from the request
// header, reusing it if present or generating a new one. The ID is stored
// in the request context and echoed in the response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(headerKey)
		if id == "" {
			id = generateRequestID()
		}
		w.Header().Set(headerKey, id)
		next.ServeHTTP(w, r.WithContext(storeRequestID(r.Context(), id)))
	})
}
