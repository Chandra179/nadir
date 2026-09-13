# Qdrant history Adapter

Persists Conversation sessions and turns in a dedicated Qdrant collection.
It owns payload encoding, vectorization of history records, and Qdrant errors;
Chat owns mutation ordering, revision checks, and deletion policy.

Change here for the history storage protocol or schema. Verify with
`go test ./internal/adapters/qdrant/history` and integration tests when the
collection contract changes.
