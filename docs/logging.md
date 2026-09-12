# Logging Standards

Level and content rules for logs. Builds on [`errors.md`](errors.md) (wrapping
on the way up) — this covers what happens once an error reaches a logger. We
use [`go.uber.org/zap`](https://pkg.go.dev/go.uber.org/zap), configured in
`logger/logger.go`.

---

## Rules

* **One line per normal request.** All request-scoped completion logging happens
  in `RequestLog` (`middleware/request_log.go`) — don't log elsewhere. Recovery
  logs panics separately because Gin recovers before the completion middleware
  can resume.
* **Never log request/response bodies, at any status.** A decoded body
  isn't what the client sent anyway, and 4xx bodies are exactly where
  secrets (a failed login's password) show up. Use the request ID to
  correlate instead.
* **Never log secrets** — auth headers, tokens, passwords, API keys — at
  any level.
* **Query logging is opt-in.** Production disables it. When enabled, only
  configured allowlisted keys are included, and keys containing sensitive
  fragments are logged as `[REDACTED]`.
* **Log route patterns, not arbitrary URL paths.** Unmatched requests are
  recorded as `<unmatched>` so path parameters cannot accidentally become log
  fields.

## Surfacing an error from a handler

Call `c.Error(err)` before writing the response — `RequestLog` logs it, you
never call the logger yourself:

```go
if err != nil {
	_ = c.Error(fmt.Errorf("get example %s: %w", id, err)) // wrap per errors.md
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	return
}
```

## Stack traces

zap attaches these automatically by level. The recovery middleware includes a
stack because it is converting a panic into an error response; never add one in
a handler:

* **Production**: `Error`+ only (5xx, panics).
* **Development**: `Warn`+ (4xx too).

The configured `logger.level` controls the zap threshold. Production uses
structured JSON output and the configured access-log sampling policy. Sampling
is an operational volume control, not a replacement for unsampled audit logs.
