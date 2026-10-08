# Semantic cache policy

Owns semantic cache hit/miss behavior, embedding-version compatibility, TTL,
and invalidation generations. The cache never calls a model: Retrieval embeds
the question once and passes its vector to `Get` and to the prepared write,
together with the request size that lets a short but complete result set hit. Persistence is injected through a private,
consumer-owned backend seam; the Qdrant implementation belongs in
`providers/qdrant/cache`.

Change here for cache semantics. Change the Qdrant Adapter for storage
protocols or add another backend for a different persistence system. Verify
with `go test -race ./internal/core/retrieval/cache`.

`interface.go` exposes only `SemanticCache`; the persistence backend seam is
private in `private_interfaces.go`.
