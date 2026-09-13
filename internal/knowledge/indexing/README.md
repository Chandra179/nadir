# Document indexing

Owns document intake, source discovery, SHA deduplication, chunk → enrich →
embed planning, bounded retries, versioned replacement, cache invalidation,
and reset coordination.

Change here for document lifecycle or indexing policy. Use `chunking/` for
boundaries and `adapters/` for providers/storage. Verify with
`go test -race ./internal/knowledge/indexing`.

`interface.go` contains the public `Ingest` contract. Consumer-only seams for
storage, lifecycle, conversion, cache invalidation, and optional batching live
in `private_interfaces.go`.
