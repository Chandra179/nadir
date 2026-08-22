# Logging Standards

Level and content rules for logs. Builds on [`errors.md`](errors.md) (wrapping
on the way up) — this covers what happens once an error reaches a logger. We
use [`go.uber.org/zap`](https://pkg.go.dev/go.uber.org/zap), configured in
`logger/logger.go`.

---

## Rules

* **One line per request.** All request-scoped logging happens in
  `RequestLog` (`middleware/request_log.go`) — don't log elsewhere.
* **Never log request/response bodies, at any status.** A decoded body
  isn't what the client sent anyway, and 4xx bodies are exactly where
  secrets (a failed login's password) show up. Use the request ID to
  correlate instead.
* **Never log secrets** — auth headers, tokens, passwords, API keys — at
  any level.

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

zap attaches these automatically by level — never add one by hand
(`debug.Stack()` in a handler, etc.):

* **Production**: `Error`+ only (5xx, panics).
* **Development**: `Warn`+ (4xx too).