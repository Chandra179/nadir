# Logging

The backend uses Go `log/slog` from `internal/bootstrap/logging`. `middleware.logger.level` accepts the existing `dev` and `prod` values; both preserve the old effective Info threshold, and both emit structured JSON. Domain operations use child operation IDs through `internal/core/observability`.

`internal/bootstrap/httpmiddleware` records one completion line per HTTP request with method, route pattern, status, duration, request ID, and trace ID. The route pattern omits path parameters. Query strings and request or response bodies are not logged. Recovery writes a separate Error line for a panic and returns HTTP 500. Provider and domain stages can log bounded operational fields, but must not log credentials or full request bodies.

The diagnostic `/debug/metrics` endpoint exposes bounded in-process counts, durations, and gate gauges. It is process-local telemetry rather than a distributed metrics store.
