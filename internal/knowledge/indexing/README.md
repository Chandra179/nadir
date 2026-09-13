# Document indexing

Owns document intake, source discovery, SHA deduplication, chunk → enrich →
embed planning, bounded retries, versioned replacement, cache invalidation,
source-root mirroring, cache invalidation, and reset coordination.

Source mirroring is explicit per indexing run. It deletes only active
Documents inside the configured roots that were absent from a successful
sweep; upload-only runs retain existing source versions, and any indexing
failure prevents reconciliation.

Change here for document lifecycle or indexing policy. Use `chunking/` for
boundaries and `adapters/` for providers/storage. Verify with
`go test -race ./internal/knowledge/indexing`.

`interface.go` contains the public `Ingest` contract. Consumer-only seams for
storage, lifecycle, conversion, cache invalidation, and optional batching live
in `private_interfaces.go`.
