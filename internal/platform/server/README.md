# Server composition

The HTTP composition root constructs the Chat, history, readiness, middleware,
and transport graph around the shared Runtime. Shared Qdrant, embedding,
indexing, cache, and retrieval construction belongs in `../runtime/`. This
Module starts the HTTP server and coordinates shutdown. Business rules belong
in their owning context; provider protocol details belong in Adapters.

Shutdown is bounded by `http.shutdown_timeout`: the listener stops accepting
new requests, active HTTP connections close gracefully, Chat generation is
cancelled, and detached history writes are drained before the shared runtime
is closed. The lifecycle contract has fast tests and a Qdrant-backed
`integration` test for persistence across an HTTP restart.

Change this package when dependency wiring, process lifecycle, or cross-module
startup policy changes. Verify with the API build and the affected package
tests.
