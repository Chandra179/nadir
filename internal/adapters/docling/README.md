# Document-converter Adapter

Translates the document-converter sidecar HTTP contract into the indexing
the document-conversion capability. It owns request encoding, response decoding,
timeouts, and provider errors; PDF policy belongs to `knowledge/indexing`.

Change here for the sidecar protocol or client behavior. Change
`sidecars/document-converter/` for conversion implementation and
`knowledge/indexing/` for intake policy.

Verify with `go test ./internal/adapters/docling`.
