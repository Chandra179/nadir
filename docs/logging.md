# Logging

The backend uses Go `log/slog` from `internal/bootstrap/logging`. `middleware.logger.level` accepts the existing `dev` and `prod` values; both preserve the old effective Info threshold, and both emit structured JSON. Domain operations use child operation IDs through `internal/core/observability`.

`internal/bootstrap/httpmiddleware` records one completion line per HTTP request with method, route pattern, status, duration, request ID, and trace ID. The route pattern omits path parameters. Query strings and request or response bodies are not logged. Recovery writes a separate Error line for a panic and returns HTTP 500. Provider and domain stages can log bounded operational fields, but must not log credentials or full request bodies.

The diagnostic `/debug/metrics` endpoint exposes bounded in-process counts, durations, and gate gauges. It is process-local telemetry rather than a distributed metrics store.

Chat diagnostics use operation/session/turn IDs, stage names and the bounded
`observability.ErrorLabel` classification. Neither successful rewriting nor
failure paths log the original question, rewritten question, prompt or answer.
Arbitrary provider errors are classified before being logged because they can
contain request content or credentials. Investigate failures by correlating
the operation ID and stage/error labels with local dependency readiness and
configuration. See [error handling](errors.md) for public messages.

Conversation history intentionally stores questions, answers and source
snapshots. Removing them from diagnostic logs does not remove those application
records, scrub old logs or rewrite older saved errors.
