# Middleware

Gin middleware used by the HTTP server. Registered in order via `engine.Use(...)` in `internal/platform/lifecycle/server.go`:

```
gin.Recovery → RequestID → Timeout → RequestLog → handler
```

## Change routing

Change this folder for request-wide concerns such as request IDs, deadlines,
panic recovery, or canonical request logging. Change `transport/http/` for
request decoding, validation, response mapping, routes, and SSE. Change
`platform/configuration/` for middleware settings. Middleware must not inspect
domain payloads or implement Document, Retrieval, or Conversation policy.

## Files

| File | Kind | Description |
|------|------|-------------|
| `dependencies.go` | infra | Holds the structured logger used by request logging |
| `request_id.go` | middleware | Reads/reuses `X-Request-ID` header, otherwise generates a random one. Stores the ID in context, echoes it in the response. |
| `timeout.go` | middleware | Attaches a `context.WithTimeout` deadline (from `middleware.timeout` in config) to the request context, so downstream Qdrant/Ollama calls return instead of hanging indefinitely. Source sweeps and turn SSE streams are exempt. |
| `request_log.go` | middleware | Logs one canonical line per request: method, path, status, duration, request ID, query params. Level tracks response status (Info for 2xx/3xx, Warn for 4xx, Error for 5xx); the last error attached via `c.Error(err)` is included for 4xx/5xx. Skips configured paths. Neither request nor response bodies are logged (see comment in file). |

## Why no body logging?

After JSON decoding, the body seen in middleware is not what the client sent (whitespace stripped, keys reordered, unknown fields dropped). Logging it provides no debugging value and risks PII leakage — same reasoning applies to response bodies. This includes error responses: they are exactly where sensitive input (failed login passwords, rejected payment details) is most likely to appear in the body.

## Why no RealIP middleware?

Gin's `engine.SetTrustedProxies()` + `c.ClientIP()` handle `X-Forwarded-For` / `X-Real-IP` with CIDR-based trust filtering. No custom middleware needed.

## Why no Recovery middleware?

`gin.CustomRecovery` handles panics and writes a 500 response. The recovery callback uses `zap` so stack traces go to structured logs, not stdout.

## Why no validation middleware?

`c.ShouldBindJSON(&req)` + `binding` struct tags replace the old `DecodeAndValidate[T]` helper. Keep validation logic in handlers, not middleware.

## Verification

Run `go test ./internal/platform/httpmiddleware` and verify middleware order
with `go test ./internal/platform/lifecycle` when registration changes.
