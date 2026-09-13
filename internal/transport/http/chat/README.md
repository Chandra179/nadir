# Chat HTTP transport

Maps Chat start, event-stream, cancel, and search request/response shapes to
the Conversation and Retrieval use cases. It owns JSON/SSE details only;
Chat owns event retention and generation lifecycle.

Change here for Chat API wire behavior. Verify with `go test ./internal/transport/http/chat`
and the parent HTTP end-to-end tests.
