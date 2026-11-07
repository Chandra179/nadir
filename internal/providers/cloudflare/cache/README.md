# Cloudflare Vectorize and KV semantic-cache Adapter

Implements the `retrieval/cache` persistence seam (`Find`, `Put`, `Clear`).
Vectorize stores only the query vector; the entry (results, version, timestamps)
is a JSON value in KV under `semcache/{id}`, where `id` is a deterministic hash
of the query text, so a repeated query replaces its entry. Cached result sets
are far larger than Vectorize's per-vector metadata limit, and KV expires them
natively.

- `Put` writes KV first and the vector second, so a findable vector never lacks
  its entry. KV expiration is `semantic_cache.ttl` (KV raises anything under 60
  seconds to 60 seconds); zero keeps entries until `Clear`.
- `Find` queries the single nearest vector, applies the threshold client-side
  (Vectorize has no score cutoff), then reads KV. A nearest vector whose KV entry
  has expired is a miss, and that vector is deleted so it stops shadowing others.
- `Clear` lists KV keys under the prefix (they are the vector ids), deletes those
  vectors, then the keys, page by page.
- `EnsureIndex` checks the existing index has `embedder.dimensions` and the
  cosine metric. The index and KV namespace are not created here.

Hit policy, TTL, versions and invalidation generations stay in
`retrieval/cache`. Tests run against a stateful fake of both REST APIs; they do
not prove Cloudflare's own behavior (see ADR 0042).

Change here for the storage layout. Verify with
`go test ./internal/providers/cloudflare/cache`.
