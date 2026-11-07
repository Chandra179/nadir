# Cloudflare Vectorize and D1 document-store Adapter

Persists indexed chunks and serves dense, keyword and hybrid searches for the
Indexing and Retrieval contexts, over the same seams as the Qdrant store.

| Concern | Where it lives |
|---|---|
| Dense vectors | Vectorize, one vector per chunk, id `{uuid}-g{generation}` |
| Which versions are visible | D1 `doc_versions` (`active`), the only source of truth |
| Chunk text and window text | D1 `doc_chunks` |
| Keyword (BM25) leg | D1 `doc_fts`, an FTS5 table over the contextual text |
| Corpus generation (reset) | D1 `doc_state.generation` |
| Filters (file, header, sha) | Vectorize metadata equality on hashed keys, re-checked exactly in D1; D1 `WHERE` on the keyword leg |

- **Publication:** a version is staged invisibly (inactive manifest row, chunk and
  keyword rows, then vectors, waiting for them to become queryable) and then one
  atomic D1 batch activates it and retires the others. Cleanup of retired rows
  and vectors follows; its failure is an `indexing.PublicationError`.
- **Reset:** one `UPDATE` publishes an empty generation; the old generation is
  removed afterwards and a failure there is retryable.
- **Search:** the dense and keyword legs run concurrently and are fused here with
  reciprocal-rank fusion (`k=2`). The dense leg is capped by Vectorize's topK
  limit (100 for id-only queries).
- **Setup:** the Vectorize index must exist (cosine, `embedder.dimensions`) with
  string metadata indexes `file_key`, `header_key` and `sha_key` created before
  any vector is stored. `EnsureStore` verifies this and creates the D1 tables.

Retrieval quality is not identical to the Qdrant store: the keyword leg is SQLite
FTS5 BM25 rather than hashed term vectors with IDF, and the dense index is not
tunable. Compare with the evaluator before changing the default (ADR 0043).

Change here for the storage layout or search mechanics. Verify with
`go test -race ./internal/providers/cloudflare/documents`; the tests run the SQL
on SQLite behind fakes of both APIs and do not prove Cloudflare's own behavior.
