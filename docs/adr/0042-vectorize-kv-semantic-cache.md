# 0042 — Semantic cache in Vectorize and KV

- **Status:** Accepted
- **Date:** 2026-11-07
- **Deciders:** Chandra

## Context

The semantic cache stored entries in a Qdrant collection. All cache policy lives
in `core/retrieval/cache` (hit threshold, TTL, embedding-policy version,
invalidation generations, suspension during document mutations); the store only
implements `Find`, `Put` and `Clear`. Cached result sets contain chunk text, so
one entry is far larger than Vectorize's per-vector metadata limit (10 KiB).

## Decision

- `semantic_cache.provider` selects `qdrant` (default) or `cloudflare`. The
  Adapter is `internal/providers/cloudflare/cache` over small `vectorize` and
  `kv` REST clients on the shared `cfapi` transport.
- Vectorize holds the query vector only. The entry is a JSON value in KV under
  `semcache/{id}`, `id` being the same deterministic query hash the Qdrant
  Adapter uses. Version, timestamps and result-set size live in the KV value,
  so no Vectorize metadata or metadata index is needed.
- `Put` writes KV before the vector. `Find` takes the nearest vector, applies the
  threshold client-side, then reads KV; a missing KV entry is a miss and the
  orphaned vector is deleted. KV expiration is `semantic_cache.ttl`.
- `Clear` lists KV keys (one per entry) and deletes those vectors and keys,
  because Vectorize has no delete-all. It is not atomic, which is acceptable:
  the cache policy bumps its generation before deleting and rejects any entry
  from an older generation, so physical deletion is cleanup, not correctness.
- The Vectorize index and KV namespace must already exist. `EnsureIndex` checks
  dimensions and the cosine metric and fails loudly; creating account resources
  is left to the operator. Any startup failure disables the cache and search
  continues without it, as for the Qdrant cache.
- Credentials come from the shared `cloudflare` section, plus
  `cloudflare.vectorize_index` and `cloudflare.kv_namespace_id`. The token needs
  Vectorize and KV edit access.
- Qdrant stays the default.

## Consequences

- No cache policy changes: version, TTL, generations and threshold behave as
  before because they never depended on the store.
- Vectorize applies writes asynchronously, so a new entry can take a short time
  to become findable. A repeated question asked immediately after the first may
  miss once; this only delays hits and never returns a wrong result. The
  Qdrant cache was visible immediately.
- A cache hit costs two sequential cloud round trips (Vectorize query, KV read)
  plus the query embedding, instead of one local Qdrant call. Writes are
  detached from the request, so misses are not slowed. Whether a hit still beats
  a local search plus rerank depends on network latency; measure it.
- The cache works only inside one process lifetime: policy versions embed a
  per-process epoch, so entries from earlier runs never hit. They are garbage;
  set `semantic_cache.ttl` so KV expires them (a zero TTL keeps them until a
  Clear). Their vectors are removed lazily when they are the nearest match.
- Clear cannot remove entries that KV listing has not yet shown (KV listing is
  eventually consistent) or vectors whose KV value had already expired. Such an
  orphan is rejected by the policy or removed lazily, but a stale nearest vector
  can briefly shadow a fresh neighbour.
- Every miss writes one KV value, and Workers KV limits writes to the same key to
  one per second; a free account has a small daily write quota (not checked
  here). Plan quotas apply to Vectorize storage and queries too.
- Not verified against live Cloudflare: the sandbox cannot reach it. The
  Vectorize NDJSON upsert, the query and describe envelopes, whether a cosine
  score is a similarity comparable to the 0.90 threshold, and the KV raw-value,
  list and bulk-delete shapes come from SDK mirrors of the API reference. Tests
  use a stateful fake of both APIs.
