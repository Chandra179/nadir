# TODO

Prioritised, scoped improvements for the nadir retrieval engine.

---

## Tier 1 — Now (high urgency, low effort)

### 1.1 Fail-open: reranker error → soft fallback

**Problem:** If the Python cross-encoder sidecar returns an error or timeout, `postProcess` in
`internal/search/search.go:68-71` fatally propagates the error, returning 500 to the client.
The dense+BM25 results are discarded entirely even though they are perfectly usable.

**Fix:** Replace the fatal error with a logged warning and return the pre-rerank chunks.

**Files:**
- `internal/search/search.go` — `postProcess` method (lines 66–79)

**Change:**
```
reranked, err := s.reranker.Rerank(ctx, query, chunks)
if err != nil {
    // log warning, fall back to un-reranked chunks
    break  // skip rerank, keep chunks as-is
}
```

**Considerations:** The reranker is optional (gated by `reranker.enabled`). This change
makes it *defensively optional at runtime* as well.

---

### 1.2 Fail-open: BM25 leg failure → dense-only

**Problem:** `hybridSearchClient` in `internal/store/store_qdrant.go:188` calls dense search
then BM25 search. If BM25 fails, the entire function returns an error and the already-fetched
dense results are discarded.

**Fix:** If the BM25 leg fails, log the error and proceed with only the dense leg for RRF.

**Files:**
- `internal/store/store_qdrant.go` — `hybridSearchClient` method (lines 188–244)

**Change:**
```
bm25Results, err := s.KeywordSearch(ctx, query, fetchN, filter)
if err != nil {
    // log warning: bm25 leg failed, using dense-only results
    // skip RRF merge, return denseResults directly
}
```

**Considerations:** Same pattern applies in reverse (dense fails, BM25 survives), though
dense failure is less likely since it requires the embedder to have already succeeded.

---

## Tier 2 — Soon (small, capped scope)

### 2.1 Semantic cache flush on re-ingest

**Problem:** When source files are edited and re-ingested, the semantic cache still serves
stale results from the old chunks. TTL-based expiry (24h default) is read-only — stale
entries accumulate in the Qdrant cache collection.

**Fix:** Add a `Clear` method to `SemanticCache` that deletes all points in the cache
collection. Call it at the start of the ingest pipeline before processing files.

**Files:**
- `internal/cache/cache.go` — new `Clear(ctx) error` method
- `internal/httpserver/server.go` — thread `*cache.SemanticCache` into `IngestHandler`
- `internal/httpserver/ingest.go` — call `cache.Clear(ctx)` before `svc.Run(ctx)`

**`Clear` implementation:**
```go
func (c *SemanticCache) Clear(ctx context.Context) error {
    _, err := c.points.Delete(ctx, &qdrant.DeletePoints{
        CollectionName: c.name,
        Points: &qdrant.PointsSelector{
            PointsSelectorOneOf: &qdrant.PointsSelector_Filter{
                Filter: &qdrant.Filter{},  // matches all
            },
        },
    })
    return err
}
```

**Considerations:**
- Full flush is simple and correct; targeted eviction (by source file) would require a
  reverse index (`file_path → [query_ids]`) which is disproportionate for this codebase.
- A flush during a concurrent search will cause a cache miss that repopulates. This is
  acceptable — cache is an optimisation, not a source of truth.
- Alternative: skip the flush and instead tag each cache entry with the set of
  `source_sha` values it references; evict on SHA mismatch. Significantly more complex.

---

### 2.2 Reranker semaphore / bulkhead

**Problem:** 50 concurrent `/search` requests generate 50 concurrent calls to the Python
cross-encoder sidecar (1 GB memory limit in `docker-compose.yml`). Under load this
causes OOM kills or degraded inference latency.

**Fix:** Add a weighted semaphore (channel-based) inside `HTTPReranker.Rerank` to cap
concurrent outbound calls. When the semaphore cannot be acquired within a deadline,
skip reranking and return original results (fail-open).

**Files:**
- `internal/reranker/reranker.go` — add semaphore and context-aware acquire

**Design:**
```go
type HTTPReranker struct {
    addr       string
    client     *http.Client
    sem        chan struct{}
}

func NewHTTPReranker(addr string, maxConcurrent int) *HTTPReranker {
    return &HTTPReranker{
        addr: addr,
        client: &http.Client{Timeout: 30 * time.Second},
        sem:   make(chan struct{}, maxConcurrent),
    }
}

func (r *HTTPReranker) Rerank(ctx context.Context, ...) {
    select {
    case r.sem <- struct{}{}:
        defer func() { <-r.sem }()
    case <-ctx.Done():
        return chunks, nil  // fail-open: skip rerank
    }
    // ... existing HTTP call ...
}
```

**Config:** Add `reranker.max_concurrent` to `config/config.yaml` (default 10).

**Considerations:**
- The semaphore caps outbound concurrency only; inbound HTTP goroutines still queue at
  Go's network poller. This is the correct boundary — we protect the sidecar, not the
  HTTP server.
- Fail-open (return original scores) is better than blocking indefinitely, as it avoids
  cascading timeout failures.

---

## Tier 3 — Worthwhile (larger scope, plan first)

### 3.1 Parallel multi-fragment search

**Problem:** `multiSearch` in `internal/search/search.go:81-109` processes query fragments
sequentially in a for-loop. For a query like *"What is the secant formula? How does it
relate to Newton's method?"*, each fragment is embedded and searched one at a time,
multiplying latency linearly.

**Fix:** Execute fragment searches concurrently using an error group with bounded
goroutines.

**Files:**
- `internal/search/search.go` — `multiSearch` method

**Change:**
```
g, ctx := errgroup.WithContext(ctx)
g.SetLimit(4)  // max parallel fragments
mu := sync.Mutex{}
for _, frag := range fragments {
    frag := frag
    g.Go(func() error {
        vec, err := s.embedder.Embed(ctx, frag)
        // ...
        results, err := s.store.HybridSearch(ctx, vec, frag, topK, filter)
        mu.Lock()
        // merge into seen
        mu.Unlock()
        return nil
    })
}
if err := g.Wait(); err != nil { return nil, err }
```

**Considerations:**
- Bounded concurrency (4) prevents overwhelming Qdrant or the embedder.
- `errgroup` naturally cancels all remaining fragments on first error.
- Test with single-fragment queries (no goroutine overhead).

---

### 3.2 Auto-reingest via file watcher

**Problem:** Ingestion is manual (POST `/ingest`). Users editing source files must
remember to trigger re-ingestion, or the index and cache become stale.

**Fix:** Add a file watcher goroutine using `fsnotify` that watches all `source.paths`
directories. On `Write` / `Create` / `Remove` events for `.md` files, trigger the ingest
pipeline for the affected file(s).

**Files:**
- `internal/ingest/watcher.go` — new file; `FileWatcher` struct
- `cmd/server/main.go` — start watcher goroutine alongside HTTP server

**Design:**
```go
type FileWatcher struct {
    paths     []string
    processor Processor
    store     Store
    log       logger.Logger
}

func (w *FileWatcher) Start(ctx context.Context) error {
    watcher, _ := fsnotify.NewWatcher()
    for _, p := range w.paths { watcher.Add(p) }
    // debounce events (50ms window)
    // on write/create: ingest the file
    // on remove: delete from store and flush cache
}
```

**Considerations:**
- `fsnotify` is the standard library, no external dependency needed (it's in the
  vendored toolchain).
- Debounce is essential — text editors emit multiple events per save.
- Symlink handling and recursive watching need attention.
- This makes the system genuinely reactive and eliminates the need for scheduled
  re-ingestion.

---

### 3.3 Instrumentation (metrics)

**Problem:** There is a `prometheus.yml` and `recording_rules.yml` in `config/` but zero
metrics instrumentation in the Go code. No way to observe p50/p99 latency, error rates,
cache hit rates, or reranker queue depth.

**Fix:** Add Prometheus metrics using `prometheus/client_golang` (already in the Go
ecosystem). Expose on a separate `/metrics` endpoint.

**Files:**
- `internal/httpserver/metrics.go` — new file
- `internal/httpserver/server.go` — register metrics route
- `internal/search/search.go` — instrument search latency and error count
- `internal/reranker/reranker.go` — instrument call count, duration, fallback count
- `internal/cache/cache.go` — instrument hit/miss counters

**Key metrics:**
```
search_duration_seconds{type="semantic|keyword"} histogram
search_errors_total{type="embed|qdrant|reranker|bm25"} counter
reranker_calls_total{status="ok|fallback|error"} counter
reranker_queue_depth gauge
cache_hits_total counter
cache_misses_total counter
cache_entries gauge
ingest_duration_seconds histogram
ingest_files_total{status="processed|skipped|failed"} counter
```

**Considerations:**
- Keep cardinality low — no per-query labels.
- `promauto` makes registration succinct.
- Coordinate with the existing `prometheus.yml` if a Prometheus server is already
  scraping this endpoint.

---

## Tier 4 — Future (needs design spec first)

### 4.1 Generic metadata / RBAC pre-filtering

**Need:** Users want to restrict search results by document attributes (owner, team,
classification level, directory path expressions).

**Gap:** `ScoredChunk` has only 6 fixed payload fields. `SearchFilter` has 3 hardcoded
conditions. Neither is extensible.

**What would be required:**

1. **Data model:** Add a generic metadata bag (e.g. `map[string]string` or `[]string` tags)
   to `ScoredChunk`. Persist it in Qdrant payload during `Upsert`.

2. **Ingest:** Extract metadata from files — directory-derived tags, file-level YAML
   front matter, or a companion `.metadata.yaml` file per source dir.

3. **API:** Extend the search request body with a `filter.metadata` map. The map keys
   map to payload field names; values are matched as Qdrant keywords.

4. **Store:** Widen `buildFilterConditions` to handle arbitrary key-value pairs.

5. **Auth:** Decide how user identity reaches the handler — HTTP header, JWT claim, or
   request body field. This has security implications beyond the search engine.

**Do not start without:** A concrete RBAC model (roles, groups, inheritance rules)
and a decision on identity propagation. The plumbing is straightforward; the design
decisions are not.

---

### 4.2 Semantic cache: targeted invalidation

**Current state:** Cache is a flat `query → results` map with no link to source files.
The Tier-2 fix (flush on re-ingest) is correct but blunt.

**Improvement:** Tag each cache entry with the `source_sha` values of all chunks
included in its results. On re-ingest, only evict entries whose referenced SHAs
have changed.

**Requires:**
- Add `source_shas []string` payload field to cache entries (populated at `Set` time
  from the chunk data).
- Add a `DeleteByFilter` method to `SemanticCache`.
- On re-ingest, collect changed SHAs, then evict cache entries that reference them.

**Trade-off:** More CPU/memory at cache-write time to deduplicate SHAs. More Qdrant
operations at re-ingest time. Only worthwhile if the full-flush latency (seconds) is
unacceptable in production.

---

## Near-term delivery order

```
Week 1:
  [1.1] Fail-open: reranker fallback       ← 30 min
  [1.2] Fail-open: BM25 leg fallback       ← 15 min
  [2.1] Cache flush on re-ingest           ← 1 hr
  [2.2] Reranker semaphore                 ← 1 hr

Week 2:
  [3.1] Parallel multi-fragment search     ← 2 hr
  [3.2] Auto-reingest via file watcher     ← 4 hr
  [3.3] Prometheus instrumentation         ← 3 hr
```

Each item is independently shippable. None depends on another.
