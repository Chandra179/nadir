# Production Readiness: nadir PKB

> Analysis date: 2026-04-25. Current branch: main.

---

## TL;DR

**10,000 documents: YES, can handle.** Qdrant scales to millions of vectors; 10k docs × ~20 chunks = ~200k points — trivial for Qdrant. Bottleneck is ingest speed (Ollama embed throughput), not storage.

**Production-grade: NO.** Missing observability, rate limiting, health checks, graceful shutdown, and Docker networking. Fix those before exposing publicly.

**Observability first or pipeline first?** Observability first. Flying blind in prod = can't diagnose when things break.

---

## What Already Works at Scale

| Concern | Status | Evidence |
|---------|--------|----------|
| Vector storage | ✅ | Qdrant single-node handles millions of points |
| SHA-based dedup | ✅ | `GetAllFileSHAs` = single paginated scroll, not O(N) RPCs |
| Payload indexes | ✅ | `file_path` keyword index + `text` full-text index in `EnsureCollection` |
| Concurrent ingest | ✅ | 8-worker semaphore pool in `IngestHandler` |
| Batch embedding | ✅ | `BatchEmbedder` interface; Ollama `/api/embed` = 1 HTTP round-trip per file |
| Concurrent dense+sparse | ✅ | `sync.WaitGroup` per chunk |
| Retry logic | ✅ | Exponential backoff in `Pipeline`, not in Embedder/Store |
| gRPC to Qdrant | ✅ | Lower overhead than REST |

---

## 10,000 Documents: Capacity Math

Assumptions: avg 5 chunks/doc at 512 tokens, 768-dim float32.

| Metric | Value |
|--------|-------|
| Total chunks | ~50,000–200,000 (varies by doc size) |
| Vector storage | 200k × 768 × 4 bytes = ~614 MB |
| Qdrant RAM (HNSW + payload) | ~2–3 GB |
| Ingest time (Ollama, single-node) | ~45–90 min (Ollama batch: ~500 chunks/min) |
| Search latency | < 50ms (vector search), + 100–400ms reranker |

**Qdrant handles this easily.** Ollama local embedding is the real ingest bottleneck.

---

## Critical Gaps Before Production

### 1. Observability — HIGHEST PRIORITY

**Currently blind.** No metrics, no traces, no dashboards.

What you need:

```
- Cache hit rate (semantic cache)
- Embed latency p50/p90/p99
- Ingest throughput (chunks/sec)
- Rerank delta (did reranker improve order?)
- Qdrant query latency
- HTTP request latency + error rate
- Failed/skipped/processed counts (already in IngestResponse — just not exported)
```

Options (cheapest first):
- `expvar` — zero deps, Go stdlib, HTTP endpoint at `/debug/vars`
- Prometheus + Grafana — standard; add `prometheus/client_golang`
- OpenTelemetry — future-proof; more setup

**Recommendation:** Start with `expvar` for counters (cache hits, embed latency, ingest stats). Add Prometheus when you want dashboards.

### 2. Docker Networking — BROKEN IN COMPOSE

`config.yaml` has `localhost` addresses. In Docker Compose, `app` container cannot reach `localhost:11434` (Ollama) or `localhost:6334` (Qdrant) — those resolve to the container itself.

```yaml
# docker-compose.yml missing:
# - Ollama service (or host.docker.internal for local Ollama)
# - app → qdrant service name instead of localhost
# - app → ollama via host.docker.internal or service name
```

Fix:
```yaml
services:
  app:
    environment:
      - QDRANT_ADDR=qdrant:6334
      - OLLAMA_ADDR=http://host.docker.internal:11434
  qdrant:
    # already correct
```

Or pass these via `.env` and read in config. Currently `docker compose up` will start but all embed/search calls will fail.

### 3. No Health Check on App Container

Qdrant has a healthcheck. App container has none. Load balancers and orchestrators (k8s, ECS) need this.

```dockerfile
# Add to Dockerfile
HEALTHCHECK --interval=10s --timeout=3s \
  CMD wget -qO- http://localhost:8080/health || exit 1
```

Add `GET /health` handler that pings Qdrant + returns 200.

### 4. No Graceful Shutdown

`cmd/http/main.go` presumably does `http.ListenAndServe` without shutdown context. Mid-ingest termination = partial state in Qdrant.

```go
srv := &http.Server{...}
quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
<-quit
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
srv.Shutdown(ctx)
```

### 5. Rate Limiting — MISSING

No throttle on `/ingest` or `/search`. Single user spamming `/ingest` can saturate Ollama + Qdrant.

For single-node: `golang.org/x/time/rate` token bucket, ~10 lines.

```go
limiter := rate.NewLimiter(rate.Every(time.Second), 10) // 10 req/s burst
if !limiter.Allow() {
    http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
    return
}
```

Add to middleware chain.

### 6. Ingest Is Not Idempotent Under Concurrent Calls

Two simultaneous `/ingest` calls fetch same SHA map → both process same files → double-upsert. Upsert is idempotent in Qdrant (same UUID = overwrite) so data is correct, but wastes Ollama embed calls.

Fix: mutex or `sync.Once` per-file in handler, or reject concurrent ingest with 409.

### 7. `IngestHandler.ServeHTTP` Contains Full Business Logic

README already flags this as tech debt. At 10k docs, ingest runs for minutes. HTTP handler should kick off async job + return 202 Accepted with job ID. Client polls `/ingest/status/{id}`.

Current: synchronous, blocks until all files processed. HTTP timeout (`write_timeout: 35s`) will fire before 10k doc ingest finishes.

**This is a blocker at 10k docs.** `write_timeout: 35s` < ingest time.

Fix options:
- Bump `write_timeout` to 0 (no timeout) for ingest endpoint only
- Async job with polling endpoint (proper fix)

---

## Lower Priority (But Real)

| Gap | Impact | Fix |
|-----|--------|-----|
| Python sidecars (SPLADE, reranker) not in Dockerfile | Can't containerize full pipeline | Add `python:3.11-slim` stage or separate service |
| No structured error responses | Hard to debug from client | Already partially done via `IngestResponse.Error` |
| `eval` in `_test.go` | Can't run eval without `go test` | Move to `cmd/eval` binary |
| Single Qdrant node | No HA | Qdrant Cloud or multi-node for prod |
| Ollama single instance | Embed bottleneck | Multiple Ollama instances behind LB |
| No auth on HTTP endpoints | Any caller can ingest/delete | Add API key middleware |

---

## Recommended Priority Order

```
1. Fix Docker networking (blocker — compose is broken today)
2. Fix write_timeout for ingest OR make ingest async (blocker at 10k docs)
3. Add /health endpoint + Dockerfile HEALTHCHECK
4. Add graceful shutdown
5. Add expvar metrics (cache hit rate, embed latency, ingest stats)
6. Add rate limiting (before any public exposure)
7. Move ingest logic out of HTTP handler → service layer
8. Add Prometheus + Grafana when you want dashboards
9. Containerize Python sidecars
10. Add API key auth
```

---

## Tradeoffs Summary

| Choice | Now | Production |
|--------|-----|------------|
| Embedding | Ollama local (private, slow) | Remote API (fast, cost+privacy tradeoff) |
| Vector DB | Qdrant single Docker | Qdrant Cloud or k8s StatefulSet |
| SPLADE/reranker | Python sidecars (manual) | Containerized services with health checks |
| Ingest | Synchronous HTTP | Async job queue (e.g. simple channel + worker) |
| Observability | None | expvar → Prometheus → OpenTelemetry |
| Auth | None | API key in middleware |
