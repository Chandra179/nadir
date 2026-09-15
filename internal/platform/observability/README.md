# Observability helpers

Provides lightweight, provider-neutral operation telemetry shared by domain
Modules and Adapters. Each HTTP request gets a request/trace ID; Retrieval,
Chat, Indexing, reset, and cache invalidation create child operation IDs and
record bounded outcome/duration aggregates. Structured logs include the IDs,
and the API exposes the in-process snapshot at `/debug/metrics`.

The recorder intentionally uses the standard library and does not pretend to
be a distributed metrics or trace backend. OpenTelemetry/exporter wiring stays
an explicit later deployment decision. Never use query text, file paths, user
IDs, or provider error strings as metric labels.

Change here for common operation telemetry shape. Verify the platform tests,
the affected domain package tests, and the metrics endpoint behavior.
