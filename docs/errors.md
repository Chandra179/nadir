# Error handling

Providers return errors to their consuming use case. Core owns validation,
fallback and lifecycle decisions; the HTTP edge owns status codes and JSON/SSE
translation. Any layer may use `errors.Is` or `errors.As` to recognize a known
condition. Core must not import HTTP or provider implementations.

## Internal causes and public messages

Wrap errors with `fmt.Errorf("operation: %w", err)` when callers need to inspect
their cause. Keep context useful and avoid inserting questions or source text
into an error. `%v` prevents unwrapping but does **not** sanitize an error string.

Chat uses [failureMessage](../internal/core/conversation/chat/errors.go) to map
known conditions to safe explanations: a question length limit, cancellation,
timeout, or a retryable failure. Prompt-budget failures explain how to shorten
the request. Arbitrary provider text is never appended to new Chat error fields.
The same safe message reaches the live JSON response, SSE error event and saved
history. Capacity and concurrent-edit messages retain their specific guidance.

Operational diagnostics use
[ErrorLabel](../internal/core/observability/observability.go) and operation IDs.
Do not copy private questions, rewritten questions, prompts, answers, credentials
or arbitrary provider error strings into Chat logs. See [logging](logging.md).
Saved questions and admitted source snapshots remain in conversation history;
this policy does not remove them or rewrite historical records.

## HTTP example

The backend uses standard-library `net/http`. Request decoding and safe JSON
responses follow [the turn handler](../internal/edge/http/chat/search.go):

```go
var body startTurnRequest
decoder := json.NewDecoder(r.Body)
decoder.DisallowUnknownFields()
if err := decoder.Decode(&body); err != nil {
    respond.JSON(w, http.StatusBadRequest,
        map[string]any{"error": "invalid turn request"})
    return
}
```

This is a handler fragment; `startTurnRequest` and `respond.JSON` are the existing
wire type and [JSON writer](../internal/edge/http/respond/json.go). The edge maps
use-case results to the public contract without exposing internal causes.
Keep document import outcomes specific enough to explain unsupported formats,
disabled PDF intake, size limits and whether publication succeeded before a
cleanup failure; do not replace every failure with an unexplained success.

## Verification

```bash
go test -race ./internal/core/conversation/chat ./internal/core/retrieval/search ./internal/edge/http/...
```

The privacy regression injects private canaries into questions, rewrites and
provider errors and checks diagnostics, streamed errors and saved failure fields.
