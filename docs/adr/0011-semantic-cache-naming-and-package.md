# 0011 — The query-level vector cache is `SemanticCache` in `internal/cache`

- **Status:** Accepted
- **Date:** 2026-09-05
- **Deciders:** Chandra, ZCode session

## Context

The cache answers near-repeat *questions*: the query is embedded and matched
against a dedicated Qdrant collection by cosine similarity ≥ threshold; hits
skip re-retrieval. Calling that a plain `Cache` (interface `cache.Cache`) and
wondering why it depends on an embedder and Qdrant instead of memory or Redis
was a recurring confusion — the name promised key-value memoization, the
implementation is semantic matching.

Merging it into `internal/store` was also considered and rejected: the store
owns the document corpus (chunks, hybrid search, filters), while the cache
has a different lifecycle — query-level entries, TTL expiry, invalidated on
every ingest. What they share is "talks to Qdrant", not a domain.

## Decision

Keep the package as `internal/cache` (its own concern, composing embedder +
Qdrant), rename the interface to `cache.SemanticCache` so the name states the
mechanism. It is consumed by both search (lookup/store) and ingest
(invalidation) via the narrow interface.

## Consequences

- The embedder/Qdrant dependencies are self-explanatory: no embeddings, no
  semantic cache.
- The store/cache boundary follows lifecycle and domain, not infrastructure.
