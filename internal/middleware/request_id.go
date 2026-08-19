package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type contextKey string

const requestIDKey contextKey = "requestID"

const headerKey = "X-Request-ID"
const grpcMetaKey = "x-request-id"

func generateRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func storeRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func GetRequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// RequestID is a Gin middleware that reads X-Request-ID from the request
// header, reusing it if present. Otherwise it reuses the OTel trace ID from
// an already-started span (see otelgin, which must run before this), if
// one exists, or generates a random one. The ID is stored in the request
// context and echoed in the response header.
func RequestID(c *gin.Context) {
	id := c.GetHeader(headerKey)
	if id == "" {
		if sc := trace.SpanContextFromContext(c.Request.Context()); sc.IsValid() {
			id = sc.TraceID().String()
		} else {
			id = generateRequestID()
		}
	}
	c.Header(headerKey, id)
	c.Request = c.Request.WithContext(storeRequestID(c.Request.Context(), id))
	c.Next()
}

// RequestIDUnaryInterceptor is a gRPC unary server interceptor. It reads
// x-request-id from incoming metadata, reusing it if present or generating
// a new one. The ID is stored in the request context.
func RequestIDUnaryInterceptor(
	ctx context.Context,
	req any,
	_ *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	id := ""
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get(grpcMetaKey); len(vals) > 0 {
			id = vals[0]
		}
	}
	if id == "" {
		id = generateRequestID()
	}
	_ = grpc.SetHeader(ctx, metadata.Pairs(grpcMetaKey, id))
	return handler(storeRequestID(ctx, id), req)
}
