# Middleware

Gin middleware used by the HTTP server. Registered in order via `r.Use(...)`:

```
gin.CustomRecovery → otelgin.Middleware → RequestID → RequestLog → Metrics → handler
```

`otelgin.Middleware` (from `go.opentelemetry.io/contrib/.../gin/otelgin`) starts a span for every request so `RequestID` and `RequestLog` can read it from context; it's always registered, but is a no-op when tracing is disabled (`config.TracingConfig.Enabled`), since the global `TracerProvider` stays the OTel no-op default.

## Files

| File | Kind | Description |
|------|------|-------------|
| `dependencies.go` | infra | Holds `*zap.Logger` and a `*prometheus.Registry` (+ the metric vectors registered on it) for stateful middleware |
| `request_id.go` | middleware | Reads/reuses `X-Request-ID` header. If absent, reuses the OTel trace ID from the span already in context (set by `otelgin`, which runs first), falling back to a random ID if no span exists. Stores the ID in context, echoes it in the response. Also exports `RequestIDUnaryInterceptor` for gRPC and `GetRequestID(ctx)` for handlers. |
| `request_log.go` | middleware | Logs one canonical line per request: method, path, status, duration, request ID, trace ID (when a span exists), query params. Level tracks response status (Info for 2xx/3xx, Warn for 4xx, Error for 5xx); the last error attached via `c.Error(err)` is included for 4xx/5xx. Skips configured paths. Neither request nor response bodies are logged (see comment in file). Full rationale and log-level/stacktrace behavior: [`docs/logging.md`](../docs/logging.md). |
| `metrics.go` | middleware | Records an `http_request_duration_seconds` histogram and `http_requests_total` counter, labeled by method, route (`c.FullPath()`), and status. Registered on the `*prometheus.Registry` passed into `NewDependencies`, exposed by `server/http_server.go` at `/metrics` for Grafana Alloy to scrape — no Prometheus server involved. |

## Why no body logging?

After JSON decoding, the body seen in middleware is not what the client sent (whitespace stripped, keys reordered, unknown fields dropped). Logging it provides no debugging value and risks PII leakage — same reasoning applies to response bodies. See [`docs/logging.md`](../docs/logging.md) for the full policy, including why gating on status code (e.g. "log the body on 4xx") doesn't help: error responses are exactly where sensitive input (failed login passwords, rejected payment details) is most likely to appear in the body.

## Why no RealIP middleware?

Gin's `engine.SetTrustedProxies()` + `c.ClientIP()` handle `X-Forwarded-For` / `X-Real-IP` with CIDR-based trust filtering. No custom middleware needed.

## Why no Recovery middleware?

`gin.CustomRecovery` handles panics and writes a 500 response. The recovery callback uses `zap` so stack traces go to structured logs, not stdout.

## Why no validation middleware?

`c.ShouldBindJSON(&req)` + `binding` struct tags replace the old `DecodeAndValidate[T]` helper. Keep validation logic in handlers, not middleware.
