# Qdrant semantic-cache Adapter

Implements the `retrieval/cache` persistence contract with a dedicated dense
Qdrant collection. It translates cache entries to and from Qdrant payloads;
cache hit policy, TTL, versioning, and invalidation ownership remain in
`retrieval/cache`.

Change this package for Qdrant requests, collection provisioning, payload
encoding, or persistence errors. Change the retrieval cache Module for cache
semantics or a different backend contract.

## Verification

Run `go test ./internal/adapters/qdrant/cache` and the retrieval cache tests.
Run the Qdrant integration path when changing schema or collection lifecycle.
