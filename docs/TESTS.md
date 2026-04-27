# Throughput Analysis & Test Plan

> **Status:** Pre-load-test assumptions. Numbers are reasoned estimates from code + resource limits.
> Replace with measured p50/p95/p99 after running k6/wrk load tests.

---

## Search Path Anatomy

Every `/search` request passes through these serial steps (worst case, no cache):

| Step | Owner | Notes |
|------|-------|-------|
| Embed query | `OllamaEmbedder` → Ollama HTTP | 768-dim `nomic-embed-text`; ~5–20 ms local |
| Dense search | Qdrant gRPC | ANN over HNSW; sub-ms at small corpus |
| Sparse (BM25) scroll | Qdrant gRPC | Full-text index scroll; 2–10 ms |
| Client-side RRF fusion | in-process | negligible |
| Reranker (optional) | Python sidecar HTTP | +50–200 ms |
| HyDE gen (optional) | Ollama HTTP | +500–3000 ms per LLM call |
| Semantic cache hit | in-process embed → cosine | skips embed+Qdrant entirely |

**Critical path (hybrid, no rerank, no HyDE):** Ollama embed + 2× Qdrant RPCs ≈ **30–80 ms** end-to-end.

---

## Local Dev

### Environment
- Docker Compose; **no resource limits** on app/qdrant/splade
- Ollama runs on host (`host.docker.internal:11434`)
- Typically 1 CPU core available to Ollama (background process, competes with dev tools)
- Go HTTP server: stdlib `net/http`, goroutine-per-request (no artificial concurrency cap)

### Bottleneck: Ollama embed
`nomic-embed-text` on CPU: **~20–80 ms** per embed call (varies by CPU, model quantization).
Ollama processes embed requests serially by default (single-threaded model runner).

### Estimated QPS (hybrid search, no rerank, no HyDE)

| Concurrency | Estimated latency | Estimated QPS |
|-------------|-------------------|---------------|
| 1 | 50–120 ms | ~8–20 |
| 4 | 200–500 ms (Ollama queuing) | ~8–20 (same Ollama bound) |
| 8+ | degrades; Ollama queue grows | <10 effective |

**Bottleneck:** Ollama single-threaded embed. Throughput saturates at ~**10–20 QPS** regardless of concurrency.

### Cache-hit path (semantic cache enabled)
Cache hit skips embed + Qdrant. Cost: 1 Ollama embed for lookup + cosine compare in-memory.
Same Ollama bound applies, but cache-lookup embed is the only embed needed.
Effective QPS with high cache hit rate: same ~10–20 (still Ollama-bound).

### Keyword-only search (`keyword` field, no `query`)
No Ollama call. Pure Qdrant BM25 scroll.
Estimated: **50–200 QPS** (Qdrant + network round-trip only).

### HyDE enabled
Adds 1 Ollama LLM generate call (~500–3000 ms) before embed.
Effective QPS: **<2** (LLM latency dominates).

### Reranker enabled
Adds Python sidecar HTTP call (cross-encoder): +50–200 ms.
Effective QPS at concurrency=1: **5–15**.

---

## Production

### Environment (from `docker-compose.prod.yml`)
| Service | CPU limit | Memory limit |
|---------|-----------|--------------|
| app | 2 vCPU | 512 MB |
| qdrant | 4 vCPU | 8 GB |
| splade | 4 vCPU | 4 GB |
| reranker | 4 vCPU | 4 GB |

> Prod uses SPLADE (`sparseEmbedder` wired up) → server-side Qdrant `QueryPoints` hybrid, not client-side scroll.
> Ollama runs **off-box** (separate GPU host assumed). If CPU-only Ollama: same bottleneck as local.

### Bottleneck analysis

**GPU Ollama (embed on dedicated GPU):**
- `nomic-embed-text` GPU embed: ~2–5 ms
- Qdrant server-side hybrid (HNSW + sparse prefetch, RRF): ~5–20 ms
- App processing: <1 ms

Combined: **~10–30 ms** p50 at low concurrency.

**CPU Ollama (no GPU):**
- Same Ollama bottleneck as local (~20–80 ms embed)
- Throughput caps at ~10–20 QPS regardless of Qdrant/app headroom

### Estimated QPS (hybrid, no rerank, no HyDE)

| Scenario | p50 latency | Estimated QPS | Bottleneck |
|----------|-------------|---------------|------------|
| GPU Ollama, low load | 15–30 ms | **30–60** | Qdrant/network |
| GPU Ollama, high concurrency (20+) | 30–80 ms | **50–100** | Qdrant ANN + gRPC |
| CPU Ollama | 80–200 ms | **10–20** | Ollama embed |
| Cache hit (any Ollama) | 5–15 ms | **100–300** | in-process + 1 embed |
| Keyword-only | 5–15 ms | **200–500** | Qdrant gRPC |
| HyDE + GPU | 600–3000 ms | **<3** | LLM generation |
| Hybrid + reranker | 80–250 ms | **15–30** | reranker sidecar |

### App container limit: 2 vCPU / 512 MB
Go runtime uses GOMAXPROCS=2. At 100 concurrent goroutines, CPU is not the bottleneck — I/O wait dominates.
512 MB sufficient for the Go binary + connection pools (Qdrant gRPC keepalive, Ollama HTTP client).

### Qdrant: 4 vCPU / 8 GB
8 GB fits large HNSW indices (millions of 768-dim float32 vectors ≈ ~3 GB/million chunks).
4 vCPU handles concurrent ANN queries well; Qdrant is CPU-bound on HNSW traversal.
Estimated Qdrant capacity: **300–500 QPS** dense ANN queries before saturation (well above app limits).

---

## Summary: Expected Ceiling Before Load Tests

| Mode | Local QPS | Prod QPS (GPU) | Prod QPS (CPU Ollama) |
|------|-----------|----------------|-----------------------|
| Hybrid (default) | 10–20 | 50–100 | 10–20 |
| Hybrid + rerank | 5–15 | 15–30 | 5–10 |
| Hybrid + HyDE | <2 | <3 | <1 |
| Keyword-only | 50–200 | 200–500 | 200–500 |
| Cache hit | 10–20 | 100–300 | 10–20 |

**Primary bottleneck everywhere:** Ollama embed latency (serial model runner).
**Fix for higher QPS:** GPU Ollama, or swap to a dedicated embedding API (OpenAI, local vLLM batch).

---

## Planned Tests

These will replace the estimates above with real numbers.

- [ ] **k6 smoke** — 1 VU, 30s, hybrid search, measure p50/p95
- [ ] **k6 load** — ramp 1→50 VU over 5 min, find saturation point
- [ ] **k6 stress** — spike to 100 VU, measure error rate + recovery
- [ ] **keyword-only baseline** — isolate Qdrant throughput ceiling
- [ ] **cache hit rate test** — fixed query set, measure hit ratio vs QPS gain
- [ ] **reranker isolated** — POST directly to `:5002`, measure sidecar throughput
- [ ] **Ollama embed throughput** — hammer embed endpoint alone, find serial/parallel capacity
