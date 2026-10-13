# 0038 — Explicit, optional HNSW parameters for the documents collection

- **Status:** Accepted
- **Date:** 2026-10-11
- **Deciders:** Chandra

## Context

Nadir never configured HNSW. The documents collection used Qdrant's server
defaults (`m=16`, `ef_construct=100`), and Qdrant builds the graph for a
segment only after it passes `indexing_threshold` (about 20 MB, roughly 6,500
vectors at 768 dimensions). The evaluation corpora hold 29 to about 440 points,
so search was a plain full scan and there was no way to tune or even exercise
the graph.

## Decision

`qdrant.hnsw_m`, `hnsw_ef_construct`, `hnsw_full_scan_threshold`,
`indexing_threshold` and `hnsw_ef` (env `QDRANT_HNSW_*`, `QDRANT_INDEXING_THRESHOLD`)
are optional. `0` keeps the Qdrant default, so behavior is unchanged until a
value is set.

- Build parameters go on `CreateCollection` in the documents store. They apply
  to collections created afterwards: first start, or a reset generation
  (`POST /api/v1/documents/reset`). `EnsureCollection` still never alters an
  existing collection, and no `UpdateCollection` migration is added.
- `hnsw_ef` is set on the dense prefetch legs and on the standalone dense query
  of the adaptive batch. Qdrant ignores top-level `params` when a query has
  prefetch, so setting it on the fusion query would do nothing. The sparse leg
  is unaffected.
- The semantic cache (cosine threshold 0.90, limit 1) and chat-history
  collections are unchanged. Approximation could cause false cache misses, and
  history is never similarity-searched.

Collection-level HNSW settings do not touch point IDs, payloads or the public
JSON/SSE contracts. Because `hnsw_ef` can change which candidates are returned,
the `qdrant` config block is part of the cache policy version, so changing it
invalidates cached results.

## Consequences

- To exercise the graph on a small corpus, lower `indexing_threshold` and reset.
- Recall against exact search is not measured at the vector layer. Compare
  `python -m eval retrieval` before and after any tuning.
- Possible follow-up: `m=0` for the chat-history collection to skip a graph it
  never uses.
