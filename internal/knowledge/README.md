# Knowledge context

Knowledge turns source files into searchable Documents. Its ordered Indexing
pass performs intake, normalization, chunking, optional enrichment, embedding,
deduplication, and versioned publication.

| Task | Start here | Related |
|---|---|---|
| Chunk boundaries or contextual text | `chunking/` | `indexing/`, retrieval quality |
| HyPE/contextual enrichment contract | `enrichment/` | `indexing/`, Ollama enrichment |
| Ingest, dedup, retry, replace, or PDF policy | `indexing/` | all three children, Qdrant documents |

Change an existing child when its concept already exists. Add a new child only
for a new Document lifecycle with a real seam. Provider calls belong in
`adapters/`; Qdrant persistence belongs in its Adapter; HTTP multipart mapping
belongs in `transport/http/`.

## Verification

Run `go test -race ./internal/knowledge/...` for indexing or concurrency work.
Validate a changed chunking or enrichment strategy with the evaluator and a
fresh reindex.
