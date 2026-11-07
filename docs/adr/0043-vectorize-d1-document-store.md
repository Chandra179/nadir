# 0043 — Documents in Vectorize and D1

- **Status:** Accepted
- **Date:** 2026-11-07
- **Deciders:** Chandra

## Context

The document store is the deepest Qdrant dependency: dense vectors, a hashed-term
sparse vector with server-side IDF for the keyword leg, server-side RRF fusion,
payload filters, an `active` payload flag for versioned replacement (ADR 0016),
collection aliases for reset (ADR 0019) and HNSW tuning (ADR 0038). Vectorize
offers dense vectors only, no payload update, no aliases and asynchronous writes.
D1 is SQLite and supports FTS5. The Indexing and Retrieval seams are narrow
(ADR 0020), so the store can be replaced behind them.

## Decision

`documents.store` selects `qdrant` (default) or `cloudflare`. The Cloudflare
Adapter (`internal/providers/cloudflare/documents`) splits the work:

- **Vectorize** holds one vector per chunk and nothing else that is authoritative.
- **D1** holds the manifest (`doc_versions`, with `active`), the chunk text and
  window text, an FTS5 table for the keyword leg, and a one-row generation
  pointer.
- **Visibility is D1's.** A vector is only a candidate; a hit counts only if its
  version row is active in the current generation. This replaces the Qdrant
  `active` payload flag, which Vectorize cannot update.
- **Publication** stages invisibly then activates and retires in one atomic D1
  batch, so the old version stays visible until the new one replaces it.
  Staging writes D1 rows before vectors so a crash leaves rows from which cleanup
  can find every vector id, and waits (best effort, 20 s) for the vectors to
  become queryable before activating.
- **Reset** is one `UPDATE doc_state SET generation = generation + 1`, simpler and
  more atomic than the alias switch. Ids carry the generation. The old
  generation is cleaned afterwards; failure is a retryable `PublicationError`.
- **Search** runs the dense leg (Vectorize, resolved through D1) and the keyword
  leg (FTS5 `bm25`) concurrently and fuses with client-side RRF, `k = 2` to match
  Qdrant's native RRF. The query-term rules (tokenizing, question stopwords,
  contextual text) are shared with the Qdrant store in `providers/lexical`.
- **Filters** use Vectorize metadata equality on 16-character hashes of
  `file_path`, `header` and `source_sha` (a string metadata index covers only the
  first 64 bytes, and paths and headers can be longer), re-checked exactly in D1.
- `EnsureStore` verifies the index (dimensions, cosine, the three metadata
  indexes) and creates the D1 tables. It never creates the index: metadata
  indexes must exist before vectors are stored, so a missing one fails startup
  with the exact `wrangler` command.
- When no enabled component uses Qdrant it is not required: readiness skips it
  and `qdrant.addr` is not validated. Qdrant stays the default.

## Preserved

Versioned, failure-safe replacement; the immutability check for a published
version; delete by path; reset that leaves the old corpus searchable if it fails;
`GetAllFileSHAs`; exact file, header and source filters; dense, keyword and
hybrid search; adaptive-rerank component rankings; the keyword leg indexing the
contextual text; contextual enrichment and sentence windows (they only change the
text and vectors indexed); the public JSON and SSE contracts.

## Changed or lost

| Qdrant store | Cloudflare store |
|---|---|
| Keyword leg: hashed term-frequency vector with server-side IDF | SQLite FTS5 BM25 (length-normalized, saturating). Ranking differs, and so do score magnitudes |
| Adaptive-rerank margins and fusion weights tuned on Qdrant scores | Dense is cosine from Vectorize and keyword is `-bm25`; thresholds and weights tuned on Qdrant scores may need retuning. Both are off by default |
| Dense candidates `topK × prefetch_mul` | At most 100 per query (id-only queries). Larger requests get a shallower dense leg; the keyword leg is not capped |
| HNSW tuning (`qdrant.hnsw_*`, `indexing_threshold`) | Not available; setting any of them with this store is a configuration error |
| A written chunk is searchable at once (`Wait`) | Vectors become queryable asynchronously; publication waits up to 20 s, then proceeds, so the dense leg for that document can lag briefly (the keyword leg cannot) |
| One local gRPC call per search | A Vectorize query then a D1 fetch for the dense leg, in parallel with a D1 keyword query: at least two cloud round trips |
| Existing Qdrant corpus | Not migrated. Reset and reindex |

## Consequences

- Retrieval quality must be re-measured with the evaluator (ADR 0028, 0036)
  before this becomes the default. Treat the keyword-leg change as a ranking
  change, not a drop-in.
- Chunk text, window text and every query leave the machine; ingest sends all
  chunks to Cloudflare.
- D1 limits apply: 100 bound parameters per statement (a chunk insert uses 11), a
  100 KB statement size, a 30 s query limit and a per-row size limit that was not
  checked here. Vectorize limits: 100 results for id-only queries, 10 metadata
  indexes per index, 5000 vectors per upsert (the Adapter sends 200).
- Mutation waiting compares an opaque mutation id with the index's
  `processedUpToMutation`. If another writer advances the index past it first, the
  wait times out and publication proceeds; this only delays the dense leg.
- Not verified against live Cloudflare: the sandbox cannot reach it. D1 FTS5 and
  `bm25()` support, the Vectorize filter syntax, metadata-index listing, the
  `info` mutation fields, `delete_by_ids` semantics and the NDJSON upsert body
  come from SDK mirrors and third-party notes. Tests run the SQL on SQLite
  (FTS5 included) behind fakes of both APIs, which proves the logic and the
  request shapes the Adapter sends, not Cloudflare's behavior.

## Setup

1. `wrangler d1 create <name>` and `wrangler vectorize create <index>
   --dimensions=<embedder.dimensions> --metric=cosine`.
2. Before storing any vector: `wrangler vectorize create-metadata-index <index>
   --property-name=file_key --type=string`, and the same for `header_key` and
   `sha_key`.
3. Set `DOCUMENTS_STORE=cloudflare`, `CLOUDFLARE_D1_DATABASE_ID` and
   `CLOUDFLARE_DOCUMENTS_INDEX`; the token needs D1 and Vectorize edit access.
4. Unset `qdrant.hnsw_*`, start, then reindex.
