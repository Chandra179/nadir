# Logging

Constructs the process logger and its development/production formatting. It
owns logger setup, not domain event names or business decisions.

Change here for logger configuration or output policy. Verify with the
platform tests and `go vet ./internal/platform/...`.
