# Semantic cache policy

Owns semantic cache hit/miss behavior, embedding-version compatibility, TTL,
and invalidation generations. Persistence is injected through a private,
consumer-owned backend seam; the Qdrant implementation belongs in
`adapters/qdrant/cache`.

Change here for cache semantics. Change the Qdrant Adapter for storage
protocols or add another backend for a different persistence system. Verify
with `go test -race ./internal/retrieval/cache`.

`interface.go` exposes only `SemanticCache`; the persistence backend seam is
private in `private_interfaces.go`.
