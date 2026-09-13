# Server composition

The HTTP composition root constructs the Chat, history, readiness, middleware,
and transport graph around the shared Runtime. Shared Qdrant, embedding,
indexing, cache, and retrieval construction belongs in `../runtime/`. This
Module starts the HTTP server and coordinates shutdown. Business rules belong
in their owning context; provider protocol details belong in Adapters.

Change this package when dependency wiring, process lifecycle, or cross-module
startup policy changes. Verify with the API build and the affected package
tests.
