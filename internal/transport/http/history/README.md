# History HTTP transport

Maps session listing, session reads, and destructive history operations to the
Chat/history capabilities. It owns status codes and JSON responses; Chat owns
mutation authorization and lifecycle ordering.

Change here for session/history API wire behavior. Verify with
`go test ./internal/transport/http/history` and the parent HTTP end-to-end
tests.
