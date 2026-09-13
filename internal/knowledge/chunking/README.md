# Chunking

Turns normalized document text into bounded chunks and optional sentence
windows with source locations and headings. It owns boundary policy; indexing
owns embedding, enrichment, deduplication, and publication.

Change here for chunk boundaries or chunk metadata. Verify with
`go test ./internal/knowledge/chunking` and the retrieval evaluator after a
quality-affecting change.
