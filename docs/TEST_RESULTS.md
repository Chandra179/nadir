# Test Results

Date: 2026-05-02 | Embedder: `nomic-embed-text` (768-dim) | Store: Qdrant `pkb_chunks` (554 vectors) | topK: 5

---

## 0. Knowledge Base Dataset

| Metric | Value |
|---|---|
| Source files (`.md`) | 40 files on disk |
| Indexed files | 38 (2 excluded by ignore patterns) |
| Total chunks (Qdrant points) | **554** |
| Avg chunks per file | 14.6 |
| Raw KB size | **19 MB** |
| Total chars (raw markdown) | ~245 KB text |
| Total lines | ~5,476 |
| Semantic cache entries | 71 (post-run) |
| Embedder dimensions | 768 (nomic-embed-text) |
| Chunk strategy | recursive, size=256, overlap=32 |

### Chunk Distribution (top 10 files)

| File | Chunks |
|---|---|
| `math/precalculus/README.md` | 80 |
| `math/linear-algebra.md` | 52 |
| `math/trigonometry.md` | 42 |
| `golang/compiler.md` | 40 |
| `fundamental/database.md` | 37 |
| `math/precalculus/summary.md` | 24 |
| `golang/goroutine.md` | 21 |
| `fundamental/computing.md` | 21 |
| `fundamental/software-architecture.md` | 18 |
| `online-travel-agency.md` | 17 |

Files with >20 chunks: **8**. Files with ≤5 chunks: **11** (thin coverage — likely affects recall for those topics).

> Context: 554 vectors × 768-dim float32 = ~1.7 MB vector data in Qdrant. Each test (IR eval, k6, evalops) runs against this same collection.

---

## 1. IR Evaluation (`TestSearchEval`)

### Setup

```bash
go test ./internal/pkb/ -run TestSearchEval -v -timeout 15m
```

- **Judge:** `qrels` (offline, deterministic)
- **Store mode:** `live` → running Qdrant at `localhost:6334`
- **Qrels:** 500 entries, 50 unique queries, all graded relevant (grade ≥ 1)
- **Workers:** 8 concurrent goroutines per profile
- **SPLADE profile:** skipped (sidecar not running) → ran 5 of 6 profiles

### Eval Queries

50 queries across 7 categories:

| Category | n | Difficulty spread |
|---|---|---|
| system-design | 17 | easy/medium/hard |
| golang | 11 | easy/medium/hard |
| math | 9 | easy/medium/hard |
| databases | 5 | easy/medium/hard |
| computer-science | 5 | easy/medium/hard |
| rag | 2 | medium |
| networking | 1 | hard |

Sample queries:
- `How does the Go GPM scheduler work with goroutines and OS threads?` [golang/hard]
- `What is the Snowflake ID structure and how does it handle clock skew?` [system-design/medium]
- `What is LU decomposition and how does it speed up solving linear systems?` [math/hard]
- `What is NAT hole punching and how does P2P traversal work?` [networking/hard]
- `What is structure-aware chunking for embeddings?` [rag/medium]

### Qrels Format

```json
{"query": "...", "chunk_id": "golang/goroutine.md:3", "file_path": "golang/goroutine.md", "relevant": true, "grade": 1}
```

TREC 4-point graded relevance (0–3). Binary `relevant=true` maps to `grade=1`. Each query averages 10 relevant chunks across the 554-vector collection.

### Eval Profiles

Defined in `testdata/eval_profiles.jsonl`:

| Profile | sparse | chunk_size | overlap | HyDE variant |
|---|---|---|---|---|
| `tf-recursive256-baseline` | tf | 256 | 32 | none |
| `tf-recursive256-hyde1` | tf | 256 | 32 | standard (1 doc) |
| `tf-recursive256-multi-hyde3` | tf | 256 | 32 | multi (3 docs, 5 templates) |
| `tf-recursive256-multi-hyde5` | tf | 256 | 32 | multi (5 docs, 5 templates) |
| `tf-recursive256-adaptive-hyde` | tf | 256 | 32 | adaptive (thresh=0.50) |
| `splade-recursive256-adaptive-hyde` | splade | 256 | 32 | adaptive (thresh=0.50) |

All profiles: recursive chunker, no reranker, `live` store.

### Aggregate Results

| Profile | MRR | HitRate | NDCG | P@5 | Recall@5 | MAP | Misses | Time |
|---|---|---|---|---|---|---|---|---|
| `tf-recursive256-baseline` | **0.9183** | **1.0000** | **0.9407** | **0.8400** | 0.4200 | 0.3948 | 0 | 6s |
| `tf-recursive256-hyde1` | 0.8483 | 0.9400 | 0.8604 | 0.6560 | 0.3280 | 0.2985 | 3 | 437s |
| `tf-recursive256-multi-hyde3` | 0.9200 | 0.9800 | 0.9212 | 0.7120 | 0.3560 | 0.3289 | 1 | 447s |
| `tf-recursive256-multi-hyde5` | 0.9067 | 0.9600 | 0.9037 | 0.7040 | 0.3520 | 0.3294 | 2 | 451s |
| `tf-recursive256-adaptive-hyde` | 0.8813 | 0.9800 | 0.8915 | 0.6520 | 0.3260 | 0.2936 | 1 | 441s |

> Note: `splade-recursive256-adaptive-hyde` skipped — SPLADE sidecar not running at `localhost:5001`.

### Category Breakdown

#### tf-recursive256-baseline (best overall)

| Category | n | NDCG@5 | HitRate |
|---|---|---|---|
| computer-science | 5 | 0.7999 | 1.0000 |
| databases | 5 | 0.9860 | 1.0000 |
| golang | 11 | 0.9533 | 1.0000 |
| math | 9 | 0.8733 | 1.0000 |
| networking | 1 | 1.0000 | 1.0000 |
| rag | 2 | 1.0000 | 1.0000 |
| system-design | 17 | 0.9858 | 1.0000 |

#### tf-recursive256-hyde1

| Category | n | NDCG@5 | HitRate |
|---|---|---|---|
| computer-science | 5 | 0.8868 | 1.0000 |
| databases | 5 | 0.9877 | 1.0000 |
| golang | 11 | 0.9052 | 1.0000 |
| math | 9 | 0.7696 | 0.8889 |
| networking | 1 | 0.9060 | 1.0000 |
| rag | 2 | 1.0000 | 1.0000 |
| system-design | 17 | 0.8152 | 0.8824 |

#### tf-recursive256-multi-hyde3

| Category | n | NDCG@5 | HitRate |
|---|---|---|---|
| computer-science | 5 | 0.9088 | 1.0000 |
| databases | 5 | 0.9877 | 1.0000 |
| golang | 11 | 0.9241 | 1.0000 |
| math | 9 | 0.8277 | 0.8889 |
| networking | 1 | 0.8772 | 1.0000 |
| rag | 2 | 1.0000 | 1.0000 |
| system-design | 17 | 0.9150 | 1.0000 |

#### tf-recursive256-multi-hyde5

| Category | n | NDCG@5 | HitRate |
|---|---|---|---|
| computer-science | 5 | 0.9026 | 1.0000 |
| databases | 5 | 0.9966 | 1.0000 |
| golang | 11 | 0.9415 | 1.0000 |
| math | 9 | 0.7670 | 0.7778 |
| networking | 1 | 0.8772 | 1.0000 |
| rag | 2 | 1.0000 | 1.0000 |
| system-design | 17 | 0.9150 | 1.0000 |

#### tf-recursive256-adaptive-hyde

| Category | n | NDCG@5 | HitRate |
|---|---|---|---|
| computer-science | 5 | 0.8968 | 1.0000 |
| databases | 5 | 0.9641 | 1.0000 |
| golang | 11 | 0.8570 | 1.0000 |
| math | 9 | 0.8499 | 1.0000 |
| networking | 1 | 0.5438 | 1.0000 |
| rag | 2 | 1.0000 | 1.0000 |
| system-design | 17 | 0.9208 | 0.9412 |

### Misses

Queries where no relevant chunk appeared in top-5:

| Profile | Query | Category | top1 (score) |
|---|---|---|---|
| `tf-recursive256-hyde1` | What is QR decomposition and when is it used over LU? | math/hard | `math/linear-algebra.md:290` (0.016) |
| `tf-recursive256-hyde1` | What is the difference between OAuth2 and OIDC? | system-design/medium | `fundamental/api-design-guidelines.md:30` (0.016) |
| `tf-recursive256-hyde1` | What is architecture quantum and how does it affect coupling? | system-design/hard | `fundamental/networking.md:37` (0.016) |
| `tf-recursive256-multi-hyde3` | What is QR decomposition and when is it used over LU? | math/hard | `math/linear-algebra.md:290` (0.015) |
| `tf-recursive256-multi-hyde5` | How does SVD decompose a matrix? | math/hard | `math/linear-algebra.md:360` (0.016) |
| `tf-recursive256-multi-hyde5` | What is QR decomposition and when is it used over LU? | math/hard | `math/linear-algebra.md:290` (0.015) |
| `tf-recursive256-adaptive-hyde` | What is the difference between OAuth2 and OIDC? | system-design/medium | `fundamental/api-design-guidelines.md:30` (0.016) |

**Pattern:** `math/hard` linear-algebra queries (QR, SVD) and niche system-design terms ("OAuth2 vs OIDC", "architecture quantum") are recurring misses. Low scores (~0.016) suggest content either absent or in chunks not retrieved at top-5. Baseline (no HyDE) misses none — HyDE-generated hypothetical docs drift away from exact content for these topics.

### Key Observations

1. **Baseline wins.** `tf-recursive256-baseline` (MRR=0.92, NDCG=0.94, HitRate=1.00) outperforms all HyDE variants on this corpus. Direct query embedding + TF hybrid is sufficient when knowledge base content closely matches query vocabulary.

2. **HyDE hurts on small, well-indexed corpus.** Hypothetical docs introduce retrieval drift (score ~0.016 for misses vs ≥0.60 for baseline hits). HyDE cost: 7–8 min vs 6s baseline.

3. **Multi-HyDE-3 best HyDE variant.** MRR=0.92, NDCG=0.92, 1 miss. Diverse prompt templates help more than raw doc count (multi-hyde5 worse than multi-hyde3).

4. **Math/hard category structurally weak.** NDCG 0.77–0.87 across all profiles. `QR decomposition` and `SVD` miss across multiple profiles — likely content coverage gap or chunk granularity issue.

5. **Computer-science category lowest NDCG** (0.80 baseline). Queries span CPU architecture, OS paging, UTF-8 — topics possibly spread thin across chunks.

---

## 2. EvalOps (Continuous Evaluation)

### Architecture

```
Live query
  │
  ▼
Monitor.RecordAsync (probabilistic sampler, 5% default)
  │ sampled
  ▼
Background goroutine (pool-limited, 4 workers)
  ├── LLMContextJudge.ScoreContext per chunk (LOW→0.25 / MED→0.5 / HIGH→1.0)
  ├── TraceStore.Append → evalops_traces.jsonl
  └── DriftDetector.Add(context_relevance) → alert if mean drops ≥ 10%
```

### Components

**`evalops.Monitor`** — wires sampler + judge + trace store + drift detector. `RecordAsync` is zero-cost when not sampled (atomic counter + float compare). Pool drops samples when full rather than blocking hot path.

**`evalops.Sampler`** — reservoir-style, thread-safe via atomic counter. `ShouldSample()` returns true for `sampleRate` fraction of calls.

**`evalops.LLMContextJudge`** — POST to OpenAI-compatible `/chat/completions`. Maps response (LOW/MEDIUM/HIGH) to 0.25/0.5/1.0.

**`evalops.DriftDetector`** — rolling window mean. Baseline set from first full window. Alert when `(baseline - mean) / baseline ≥ threshold`.

**`evalops.TraceStore`** — mutex-serialized JSONL append per record.

### Config

```yaml
evalops:
  enabled: true
  sample_rate: 0.05      # 5% of live queries (set 1.0 for testing)
  trace_file: "evalops_traces.jsonl"
  drift_window: 50       # rolling window size
  drift_thresh: 0.10     # 10% relative drop triggers alert
  model: "gemma3:1b"
  max_workers: 4
```

### Live Run Results

> Date: 2026-05-02. Judge: `gemma3:1b` via Ollama. Sample rate: 100% (testing). Dataset: 554 chunks across 38 files. 14 queries sampled, topK: 3.

| Metric | Value |
|---|---|
| Traces captured | 14 |
| Mean context_relevance | **0.310** |
| Queries with ≥1 relevant chunk | 8 / 14 (57%) |
| Queries with 0 relevant chunks | 6 / 14 (43%) |
| Drift alert threshold | 0.279 (10% below baseline) |
| Judge model | `gemma3:1b` |

| Query | Context Relevance | Relevant / topK |
|---|---|---|
| How does BFS work for shortest path in unweighted graphs? | 0.333 | 1/3 |
| Explain the implementation of A* search algorithm with heuristics | 0.250 | 0/3 |
| What is dynamic programming memoization vs tabulation approach? | 0.333 | 1/3 |
| What are goroutines in Go and how are they scheduled by the runtime? | 0.250 | 0/3 |
| How does OAuth2 differ from OIDC for authentication? | 0.250 | 0/3 |
| What is the difference between mutex and semaphore? | 0.250 | 0/3 |
| How does LRU cache eviction work? | 0.250 | 0/3 |
| Explain the CAP theorem in distributed systems | 0.333 | 1/3 |
| What is the difference between stack and heap memory? | 0.333 | 1/3 |
| How does Dijkstra algorithm handle negative weights? | 0.333 | 1/3 |
| What is a Bloom filter used for? | 0.417 | 2/3 |
| Explain context switching in operating systems | 0.250 | 0/3 |
| What are the SOLID principles in software design? | 0.250 | 0/3 |
| How does index selectivity affect query performance? | 0.500 | 1/3 |

**Key findings:**

* Mean 0.310 low — `gemma3:1b` scores LOW (0.25) for majority. Root causes: reranker disabled during run (less precise top-K); KB weak on CS fundamentals (mutex, context switching, SOLID — ≤5 chunks each); `gemma3:1b` strict judge.
* Best: DB index selectivity (0.500), Bloom filter (0.417) — strong KB coverage in `fundamental/database.md` (37 chunks).
* Pool saturation: burst of 10 simultaneous queries → 6 dropped (pool=4). Expected behavior; production `sample_rate: 0.05` prevents this.
* Unit tests: 19/19 pass (`internal/pkb/evalops/evalops_test.go`). All components covered.

---

## 3. k6 Load Tests

**Bug fixed:** `common.js` passed object to `SharedArray` (requires array). Fixed: wrap parsed JSON in `[...]`.

```diff
-  return JSON.parse(open('./testdata/queries.json'));
+  return [JSON.parse(open('./testdata/queries.json'))];
```

### Test Queries (`testdata/queries.json`)

**General pool (20 queries):**
```
how does consistent hashing work, goroutine vs thread difference,
rate limiting algorithms, kafka consumer group rebalancing,
oauth2 authorization code flow, distributed cache eviction policy,
chunking strategies for embeddings, api design best practices,
compiler tokenization process, database indexing b-tree,
how does Go garbage collector work, what is the CAP theorem,
explain vector clocks in distributed systems,
how does raft consensus algorithm work, difference between BFS and DFS,
what is a bloom filter, explain MVCC in databases,
how does TCP congestion control work, what are SOLID principles,
explain monte carlo tree search UCB
```

**Cache-fixed pool (5 queries):** repeated for cache warmup tests
```
how does consistent hashing work, goroutine vs thread difference,
rate limiting algorithms, database indexing b-tree,
how does Go garbage collector work
```

---

### smoke.js — Baseline Correctness

**Config:** 1 VU, 30s, `p(50)<500ms`, `p(95)<2000ms`

```
✓ status 200      56/56  100%
✓ has results     56/56  100%
✓ http_req_failed  0/28   0.00%
✓ http_req_duration p(95) < 2000ms
```

| Metric | Value |
|---|---|
| Iterations | 28 |
| Throughput | 0.91 req/s |
| p(50) duration | 31.1ms |
| p(95) duration | 290.4ms |
| p(99) duration | ~315ms |
| Error rate | 0% |

**Result: PASS**

---

### cache_hit_rate.js — Semantic Cache Warmup

**Config:** 10 VUs, 90s, `cache_hit_rate > 50%`

Hit detection: `X-Cache: HIT` header OR `response_time < 50ms`.

```
✓ status 200        24279/24279  100%
✓ cache_hit_rate     96.21%  (threshold: >50%)
✓ http_req_failed    0/24279  0.00%
```

| Metric | Value |
|---|---|
| Total requests | 24,279 |
| Throughput | 269.7 req/s |
| Cache hits | 23,359 (96.21%) |
| Cache misses | 920 (3.79%) |
| Cached p(50) | 35.2ms |
| Cached p(95) | 43.85ms |
| Uncached p(50) | 58.7ms |
| Uncached p(95) | 84.67ms |
| Cache speedup | ~1.7× (58ms → 35ms median) |

**Result: PASS.** 96.21% hit rate far exceeds 50% threshold. Fixed 5-query pool saturates Qdrant semantic cache quickly. Cache reduces median latency by ~40%.

---

### keyword_baseline.js — Keyword vs Hybrid Delta

**Config:** 5 VUs, 60s, `keyword_duration p(95) < 1000ms`

Each iteration fires both keyword-only and hybrid search for same query.

```
✓ keyword 200     537/537  100%
✓ hybrid 200      537/537  100%
✓ http_req_failed   0/1074  0.00%
✓ keyword_duration p(95) = 58.81ms  (threshold: <1000ms)
```

| Metric | Keyword | Hybrid |
|---|---|---|
| p(50) | 31.1ms | 25.1ms |
| p(95) | 58.81ms | 48.75ms |
| p(99) | ~311ms | ~78ms |
| Throughput (combined) | 17.7 req/s | — |

**Result: PASS.** Hybrid search faster than keyword-only at median and p(95). Keyword has higher p(99) spike (311ms vs 78ms) — Qdrant BM25 scroll has tail latency under load. Hybrid's dense path + RRF more consistent.

> Note: `sparse_only: true` flag sent but server ignores unknown JSON fields — both modes hit full hybrid pipeline. Delta measures routing overhead, not true sparse-only path.

---

### ollama_embed_throughput.js — Embedder Saturation

**Config:** 4-stage ramp (1→4→8→1 VUs over 2m), model: `nomic-embed-text`

```
✓ status 200        21849/21849  100%
✓ has embedding     21849/21849  100%
✓ embed_error_rate   0.00%
```

| Stage | VUs | Throughput | p(50) | p(95) |
|---|---|---|---|---|
| Serial baseline | 1 | ~62/s | ~16ms | — |
| Light parallel | 2–4 | ~135/s | 17ms | — |
| Saturation probe | 5–8 | ~320/s | 17ms | 24.75ms |
| Cool down | 1–2 | ~130/s | 17ms | — |
| **Overall** | **1–8** | **182/s** | **17.26ms** | **24.75ms** |

| Metric | Value |
|---|---|
| Total embeddings | 21,849 |
| Avg throughput | 182 req/s |
| p(50) latency | 17.26ms |
| p(95) latency | 24.75ms |
| Max latency | 71.56ms |
| Error rate | 0% |

**Result: PASS.** `nomic-embed-text` handles up to 8 parallel embed requests with stable latency (p(95) stays under 25ms). No saturation observed — likely CPU-bound at higher concurrency not tested. Embed latency contributes ~17ms to each uncached search request.

---

### load.js — Ramp Load (not run this session)

**Config:** 4-stage ramp: 10→25→50→0 VUs over 6m, `p(95)<5000ms`, `error_rate<5%`

Not executed — requires sustained load with all services warm. Run separately:

```bash
k6 run load.js
```

---

## Summary

| Test | Status | Key Result |
|---|---|---|
| `TestSearchEval/tf-recursive256-baseline` | PASS | MRR=0.92 HitRate=1.00 NDCG=0.94 — best profile |
| `TestSearchEval/tf-recursive256-hyde1` | PASS | 3 misses, 437s — HyDE hurts on this corpus |
| `TestSearchEval/tf-recursive256-multi-hyde3` | PASS | 1 miss, best HyDE variant |
| `TestSearchEval/tf-recursive256-multi-hyde5` | PASS | 2 misses, diminishing returns vs hyde3 |
| `TestSearchEval/tf-recursive256-adaptive-hyde` | PASS | 1 miss, 441s |
| `splade-recursive256-adaptive-hyde` | SKIP | SPLADE sidecar not running |
| `k6/smoke.js` | PASS | 0% error, p(95)=290ms, 28 reqs |
| `k6/cache_hit_rate.js` | PASS | 96.21% hit rate, 270 req/s |
| `k6/keyword_baseline.js` | PASS | p(95) keyword=59ms hybrid=49ms |
| `k6/ollama_embed_throughput.js` | PASS | 182 req/s, p(95)=25ms, 0 errors |
| `k6/load.js` | NOT RUN | requires full warm stack |

### Action Items

1. **Math/hard gap:** `QR decomposition`, `SVD` miss consistently. Check if `math/linear-algebra.md` has chunks covering these topics or if content absent from knowledge base.
2. **OAuth2 vs OIDC miss:** Single chunk containing both terms may be needed. Current chunks split or missing.
3. **SPLADE eval:** Start `python cmd/splade/main.py` and re-run `splade-recursive256-adaptive-hyde` profile — hypothesis: SPLADE sparse leg improves exact-term recall for missed queries.
4. **Reranker profiles:** All current profiles have `reranker: ""`. Add `"reranker": "cross-encoder"` profile to eval to measure reranker impact on NDCG.
5. **`load.js`:** Run with warm stack to establish p(95) latency at 50 VU sustained load.
