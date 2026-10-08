# Qdrant semantic-cache Adapter

Implements the `retrieval/cache` persistence contract with a dedicated dense
Qdrant collection. It translates cache entries to and from Qdrant payloads;
cache hit policy, TTL, versioning, and invalidation ownership remain in
`retrieval/cache`.

Change this package for Qdrant requests, collection provisioning, payload
encoding, or persistence errors. Entries carry `requested_top_k`, the result
count the cached search asked for; records without it read as 0. Change the retrieval cache Module for cache
semantics or a different backend contract.

## Verification

Run `go test ./internal/providers/qdrant/cache` and the retrieval cache tests.
Run the Qdrant integration path when changing schema or collection lifecycle.
