# Qdrant history Adapter

Persists Conversation sessions and turns in a dedicated Qdrant collection.
It owns payload encoding, the constant placeholder vector that every record
carries (Qdrant requires one, history is only listed and filtered by payload,
so nothing is embedded), and Qdrant errors;
Chat owns mutation ordering, revision checks, and deletion policy.

Change here for the history storage protocol or schema. Verify with
`go test ./internal/providers/qdrant/history` and integration tests when the
collection contract changes.
