# Production RAG

end to end rag sysyem from data ingestion -> chunking -> embed -> retrieval -> generation -> evaluation -> testing

<figure><img src="https://2576044272-files.gitbook.io/~/files/v0/b/gitbook-x-prod.appspot.com/o/spaces%2F4G3qEfKKNTPjJ3BFGqg8%2Fuploads%2FV1J2NxoC58tvnmKyaLFH%2Fimage.png?alt=media&#x26;token=439d9440-d7ec-497d-95f2-0923261a9caf" alt="" width="563"><figcaption></figcaption></figure>

## Ingestion

Accept data sources (PDFs, Markdown) -> process using Docling/Marker -> final output mardkown

* Uses Hash Tracking to compare file SHAs, ensuring only new or modified files are processed.
* Performs Targeted Deletions for modified files and uses Deterministic UUIDs to prevent duplicate entries during re-ingestion.
* Auto-Provisioning: The system self-heals by auto-configuring Qdrant collections, distance metrics, and required indices (Full-Text & Keyword) on startup.

`IngestService` runs the full loop: list → bulk SHA dedup → concurrent fetch+pipeline.

* Calls `GetAllFileSHAs` once per run (single paginated Qdrant scroll) instead of N point lookups
* 8 concurrent workers for fetch+pipeline
* Returns `IngestResult{Processed, Skipped, Failed}` counters

**Contextual Embedding:** Before embedding, each chunk is prefixed with `filePath > header\n` (Anthropic 2024). This anchors chunk semantics to document structure, improving dense retrieval accuracy without changing stored text.

Reference
- https://github.com/docling-project/docling

## Chunking Strategies

Abstraction for chunking so we can swap it depends on configuration:

1. Recursive, It first extracts sections by heading, then splits oversized sections by paragraph, then by sentence, preserving overlap between adjacent chunks.
2. Sentence Window, indexes at sentence granularity but stores a surrounding window as retrieval context

**Chunk sizes** 128, 256, 512 tokens (configurable), **Chunk overlap:** configurable

## Embedding & Storage

Needed both Sparse and Dense to be able to use RRF, configurable: dimensions.

```yaml
embedder:
  provider: "ollama"
  model: "nomic-embed-text"
  ollama_addr: "http://localhost:11434"
  dimensions: 768
  
sparse_scorer:
  provider: "prithivida/Splade_PP_en_v1"
  addr: "http://localhost:5001"
```

Qdrant for storage. store Sparse and Dense vector, example payload to store:

```json
{
  "header": "1.3 Weighted A* Search",
  "window_text": "",
  "file_path": "week 3 informed search and heuristic function.md",
  "line_start": 122,
  "chunk_index": 2,
  "source_sha": "b2f71659eee1eb2a3a377ecc1327bd9ead16552ec6c8cc101f040d187e8b8e6d",
  "text": "finds a solution in [ C ∗ , WC ∗ ], but usually closer to C ∗ .\nTo modify A* algorithm to Weighted A*, just change line 14 in Algorithm 2 to Equation 3."
}
```

#### Indexing

* **Text indexing:** Ensure full-text index on `text` field for BM25 hybrid search
* **Keyword indexing:** `file_path` payload field, eliminates full-collection scan

***

## Retrieval

cosine search, top-1 score ≥ threshold → return immediately

```
Query
  │
  ▼
Semantic Cache ──── hit (score ≥ threshold) ──► return cached result
  │ miss
  ▼
Query Transformation (HyDE)
  └── LLM generates hypothetical doc → embed → avg vector
  │
  ▼
Hybrid Search
  ├── Dense: Qdrant ANN (nomic-embed-text vec)
  └── Sparse: SPLADE or TF fallback
        └── RRF fusion (k=60): score = Σ 1/(60 + rank)
  │
  ▼
Reranker
  └── cross-encoder/ms-marco-MiniLM-L-6-v2
  │
  ▼
Results: []{ file_path, header, line_start, score, text }
```

### Semantic Cache

Caches search results keyed by query embedding similarity. A query hitting the cache at score >= threshold returns the cached result directly, skipping embedder + store + reranker round-trips. Example vector:

```json
{
  "cached_at": "2026-04-26T15:23:38Z",
  "results_json": "{Variants\",\"LineStart\":327,\"ChunkIndex\":0,\"Vector\":null,\"SparseIndices\":null}",
  "query": "In Monte Carlo Tree Search, how do we calculate UCB?"
}
```

Search and if result top-1 score ≥ threshold → return immediately, if not: run full pipeline, write result async (fire-and-forget)

* Set TTL&#x20;
* Threshold:
  * `0.85–0.90`: high recall, allows paraphrased queries
  * `0.90–0.95`: balanced (default `0.90`)
  * `>0.95`: near-identical only

### Query Transformation (HyDE)

Produces a hypothetical document for a query. Generated text is embedded and used as search vector instead of raw query. Three variants: standard, Adaptive (confidence-gated), Multi (diverse prompts). Full details in HyDE Variants section below.

Ref: "Precise Zero-Shot Dense Retrieval without Relevance Labels" (Gao et al., ACL 2023).

### Search

#### Hybrid Search

fetches dense + text-filtered candidates, reranks sparse leg client-side, fuses via RRF.

**Server-side**

Offloading the heavy lifting to Qdrant, to reduce network latency and memory overhead. It utilizes a single round-trip to execute both dense and sparse queries.&#x20;

* It uses Qdrant native RRF (Native Qdrant implementation)
* Dense search: Vector Similarity (Bi-Encoder)
* Sparse Vector Index (Inverted Index)
* Only final Top-K results sent to app

**Client-side**

Use it when you need a level of customization that a standard database engine can't provide. For example using `BM25` search and `Splade` as scorer before fusing the results. While this introduces more "noise" and latency due to the extra data transfer and manual sorting loops, it is usable for fine-tuning relevance in niche domains.

#### **Custom Search**

While Hybrid search is good its using high compute process/heavy process, i think we should consider the type of query like is the query complex, easier to understand? if using dense/keyword search resulting in good result we dont need Hybrid Search

* **Dense-Only Search** (`Search`): A standard vector similarity search without the sparse/BM25 overhead. Ideal for purely semantic queries.
* **Keyword-Only Search** (`KeywordSearch`): Bypasses vectors entirely to perform a pure text-match search using Qdrant's Scroll API. Useful for exact phrasing or when embedding models are unnecessary.

#### **Payload Filtering**

All search methods (Hybrid, Dense, and Keyword) support strict pre-filtering. This guarantees that similarity scores are only calculated against relevant documents, improving both speed and accuracy. You can filter by:

* `file_path`: Restrict searches to a specific file.
* `header`: Restrict searches to a specific section or markdown header.
* `source_sha`: Restrict searches to a specific version of a document.

### Reranking

Use a high-precision model to verify the "rough" results from the vector search.

* **Oversampling**: The retrieval stage fetches a larger set of candidates (e.g., 10x the requested amount) to ensure the reranker has enough high-quality options to choose from.
* **Contextual Scoring**: The system passes the Window Text (the chunk plus its surrounding context) to a Cross-Encoder.
* **Final Sorting**: The candidates are re-scored based on deep semantic relevance and sorted, ensuring the absolute best matches are promoted to the top for the LLM.

***

## Post-Retrieval Filtering

After reranking, an optional LLM chunk filter drops irrelevant results before generation. Ref: arxiv 2410.19572 (+10pp PopQA accuracy).

* Batches all retrieved chunks into one prompt, asks model to score each 0–1
* Drops chunks below configurable threshold (default 0.5)
* Order of surviving chunks is preserved
* Falls through on LLM error (returns all chunks rather than drop everything)

```yaml
chunk_filter:
  enabled: false
  model: "gemma3:1b"
  threshold: 0.5
```

## Generator

`OllamaGenerator` streams an answer grounded in retrieved chunks via Ollama `/api/chat`.

**Prompt construction:**

* Chunks reordered using "Lost in the Middle" principle (Liu et al. 2023): highest-scored chunk at position [1], lowest in middle, second-highest at end — reduces LLM degradation on long context
* Token budget enforced by truncating chunks (rough estimate: 1 token ≈ 4 chars, default 2800 tokens ≈ 70% of 4k context)
* System prompt requires citation inline as `[1]`, `[2]`, etc.

**Usage:** POST `/search` with `"generate": true`. Response is `text/plain` chunked transfer encoding (streaming).

```yaml
generator:
  enabled: true
  model: "phi4-mini:latest"
  max_context_tokens: 2800
```

## Evaluation & Testing

### Methodology

Offline IR eval using pre-computed ground truth (qrels). Run against multiple retrieval profiles to compare chunk size, sparse scorer, reranker, and HyDE variants. Results appended to `eval_history.jsonl` for trend analysis.

**Run:**

```bash
go test ./internal/pkb/ -run TestSearchEval -v -timeout 10m
```

**Env vars:**

| Variable              | Values                     | Default                         |
| --------------------- | -------------------------- | ------------------------------- |
| `EVAL_STORE`          | `live` / `container`       | `live` (connects to running Qdrant) |
| `EVAL_JUDGE`          | `llm` / (default)          | `qrels` (offline, from testdata) |
| `EVAL_PROFILES`       | comma-separated names      | all profiles                    |
| `EVAL_QDRANT_ADDR`    | host:port                  | from config.yaml                |
| `EVAL_QDRANT_COLLECTION` | string                  | from config.yaml                |
| `EVAL_QRELS_PATH`     | file path                  | `testdata/qrels.jsonl`          |
| `EVAL_LLM_BASE_URL`   | URL                        | from config.yaml                |
| `EVAL_LLM_MODEL`      | model name                 | from config.yaml                |

**Store modes:**

* `live` — connects to running Qdrant; assumes data already ingested; fast for iterating profiles
* `container` — spins up ephemeral `testcontainers-go` Qdrant; ingests `gitbook/` once shared across all profiles; useful for CI

### Ground Truth Format (Qrels)

Supports TREC 4-point graded relevance (`grade` field). Legacy binary `relevant` field maps to `grade=1`.

```json
{
  "query": "How does the Go GPM scheduler work with goroutines and OS threads?",
  "chunk_id": "golang/goroutine.md:3",
  "file_path": "golang/goroutine.md",
  "relevant": true,
  "grade": 2
}
```

### Metrics

| Metric            | Description                                                   |
| ----------------- | ------------------------------------------------------------- |
| MRR               | Mean Reciprocal Rank — rewards finding relevant result early  |
| HitRate (Success@K) | Fraction of queries with ≥1 relevant result in top-K        |
| NDCG@K            | Normalized Discounted Cumulative Gain — graded relevance      |
| Precision@K       | Relevant results / K                                          |
| Recall@K          | Relevant results retrieved / total relevant (needs qrels counts) |
| MAP               | Mean Average Precision — AUC over precision-recall curve      |
| ContextRelevance  | RAGAS-style avg chunk relevance score 0–1 (LLM judge only)   |

### Eval Profiles

Defined in `testdata/eval_profiles.jsonl`. Each profile specifies one retrieval config to benchmark. Fields not set inherit config.yaml defaults.

```json
{"name": "tf-recursive256-baseline", "tags": ["tf","baseline"], "sparse_scorer": "tf", "chunk_size": 256, "chunk_overlap": 32}
{"name": "tf-recursive256-hyde1", "tags": ["tf","hyde"], "sparse_scorer": "tf", "chunk_size": 256, "chunk_overlap": 32, "hyde": true, "hyde_num_docs": 1}
{"name": "tf-recursive256-multi-hyde3", "`tags": ["tf","hyde","multi-hyde"], "sparse_scorer": "tf", "chunk_size": 256, "chunk_overlap": 32, "multi_hyde": true, "hyde_num_docs": 3}
{"name": "tf-recursive256-adaptive-hyde", "tags": ["tf","hyde","adaptive"], "sparse_scorer": "tf", "chunk_size": 256, "chunk_overlap": 32, "adaptive_hyde": true, "adaptive_thresh": 0.50}
{"name": "splade-recursive256-adaptive-hyde", "tags": ["splade","hyde","adaptive"], "sparse_scorer": "splade", "chunk_size": 256, "chunk_overlap": 32, "adaptive_hyde": true, "adaptive_thresh": 0.50}
```

Profile fields: `sparse_scorer` (tf/splade), `reranker` (cross-encoder/empty), `chunk_size`, `chunk_overlap`, `chunker_provider` (recursive/sentence-window), `hyde`, `hyde_num_docs`, `adaptive_hyde`, `adaptive_thresh`, `multi_hyde`.

### Eval History

Every run appends one JSON line to `eval_history.jsonl`. Fields include timestamp, profile, model, embedder dims, topK, docs ingested, vector count, qrels totals, and all metrics. Use for regression tracking and comparing configurations over time.

### Judges

Two judge backends, selected via `EVAL_JUDGE`:

* **qrels** (default) — offline lookup against `testdata/qrels.jsonl`; fast, deterministic, no LLM required
* **llm** — calls `LLMJudge` against any OpenAI-compatible endpoint; supports `IsRelevant` (YES/NO) and `ScoreContext` (LOW/MEDIUM/HIGH → 0.25/0.5/1.0)

## HyDE Variants

Three variants, all swappable via config and eval profiles:

**Standard HyDE** — generates N hypothetical documents in parallel, averages their L2-normalized embeddings, runs hybrid search with averaged vector.

**Adaptive HyDE** — runs vanilla hybrid search first; fires HyDE only when top-1 cosine score < threshold (default 0.50). Ref: arxiv 2507.16754. Skip LLM cost when dense retrieval is already confident.

**Multi-HyDE** — cycles through 5 diverse prompt templates (factual passage, key facts, expert explanation, contextual definition, example-driven) round-robin per document generation. Maximizes embedding diversity. Ref: arxiv 2509.16369. Use with `num_docs >= 3` for benefit.

```yaml
hyde:
  enabled: true
  adaptive: true
  adaptive_thresh: 0.50
  multi_hyde: false
  model: "gemma3:1b"
  num_docs: 1
```

## Continuous Evaluation (EvalOps)

Async production monitoring: samples live search calls, judges context relevance, persists traces, detects metric drift. Ref: RAGOps arxiv 2506.03401, ARES arxiv 2311.09476.

```
Live query
  │
  ▼
Monitor.RecordAsync (probabilistic sampler, e.g. 5%)
  │ sampled
  ▼
Background goroutine (pool-limited, drops if pool full)
  ├── LLMContextJudge.ScoreContext per chunk (LOW/MED/HIGH → 0.25/0.5/1.0)
  ├── TraceStore.Append → evalops_traces.jsonl
  └── DriftDetector.Add(context_relevance) → log alert if mean drops > 10%
```

* Zero hot-path cost when not sampled (atomic counter + float compare)
* Goroutine pool (default 4) prevents unbounded background work
* `DriftDetector` uses rolling window; baseline set from first full window; alert if relative drop ≥ threshold

```yaml
evalops:
  enabled: false
  sample_rate: 0.05
  trace_file: "evalops_traces.jsonl"
  drift_window: 50
  drift_thresh: 0.10
  model: "gemma3:1b"
  max_workers: 4
```

**Trace record format:**

```json
{
  "ts": "2026-05-01T10:00:00Z",
  "query": "How does Go's GC work?",
  "chunks": [{"file_path": "golang/gc.md", "header": "GC Phases", "line_start": 45, "score": 0.87, "relevant": true}],
  "context_relevance": 0.75
}
```

## Observability

| Metric                        | Source                          |
| ----------------------------- | ------------------------------- |
| Latency (p50/p95/p99)         | Prometheus histograms           |
| Cache hit rate                | Prometheus counter              |
| Rerank score before/after     | Prometheus histogram            |
| Ingest file status            | Prometheus counter (processed/skipped/failed) |
| Embed latency + batch size    | Prometheus histogram            |
| Retrieval quality (MRR, nDCG) | Eval harness (`eval_runner.go`) |
| Context relevance drift       | EvalOps monitor (log alert)     |

## Infrastructure (Local)

Minimum services to run the full pipeline:

| Service       | Default Address         | Purpose                                    |
| ------------- | ----------------------- | ------------------------------------------ |
| Qdrant        | `localhost:6334` (gRPC) | Vector + sparse storage, BM25 index        |
| Ollama        | `http://localhost:11434` | Embedder + HyDE generator + generator LLM |
| Reranker sidecar | `http://localhost:5002` | cross-encoder/ms-marco-MiniLM-L-6-v2     |
| SPLADE sidecar | `http://localhost:5001` | Sparse vector scoring (optional)          |

**Reranker sidecar protocol** (POST `/rerank`):

```json
// request
{"query": "...", "passages": ["text1", "text2"]}

// response
{"scores": [0.95, -2.3]}
```

**SPLADE sidecar protocol** (POST `/embed_sparse`):

```json
// request
{"text": "...", "type": "query"}  // type: "query" | "passage"

// response
{"indices": [42, 99, ...], "values": [0.31, 0.12, ...]}
```

**Ollama models to pull:**

```bash
ollama pull nomic-embed-text   # embedder
ollama pull gemma3:1b          # hyde generator + chunk filter
ollama pull phi4-mini:latest   # answer generator
ollama pull llama3.1:8b-instruct-q4_K_M  # eval LLM judge
```

**Docker Compose** (`docker compose up --build`) starts the app + Qdrant. Ollama and sidecars run outside compose.

**Chunk ID scheme:** `UUIDv5(namespace, filePath:lineStart:chunkIndex)` — deterministic, avoids duplicates on re-ingest. Known namespace: `a3b4c5d6-e7f8-4a5b-9c0d-1e2f3a4b5c6d`.


---

## Session: 2026-05-02 — Latency Profiling & Optimization Run

### Environment

| Component | Value |
|-----------|-------|
| Host | 13th Gen Intel Core i5-13420H (12 CPUs, 2 threads/core) |
| RAM | 16GB total, ~7GB available |
| GPU | NVIDIA GeForce RTX 4050 Mobile (6GB VRAM, CUDA 13.1, driver 590.48.01) |
| OS | Linux 6.17.0-22-generic |
| Ollama | Running on host (not in Docker), GPU offload auto-detected |
| Qdrant | v1.13.4 (Docker, memory limit 2GB) |
| Reranker | `cross-encoder/ms-marco-MiniLM-L-6-v2` (Docker, CPU) |
| SPLADE | fastembed (Docker, CPU) |

### Config at time of test

```yaml
generator:
  model: gemma3:1b        
reranker:
  candidate_mul: 2        
qdrant:
  top_k: 5
  prefetch_mul: 5
semantic_cache:
  enabled: true
  threshold: 0.90
  ttl: 24h
hyde:
  enabled: false
chunk_filter:
  enabled: false
```

---

## Search-Only (10 queries)

Mode: `hybrid+rerank`, `top_k=5`, no generate, `generate:false`

| Query | Wall clock | Results | Top score (reranker) |
|-------|-----------|---------|----------------------|
| how does consistent hashing work | 261ms | 5 | +7.499 |
| goroutine vs thread difference | 222ms | 5 | -1.428 |
| rate limiting algorithms | 252ms | 5 | +1.650 |
| kafka consumer group rebalancing | 240ms | 5 | +3.434 |
| oauth2 authorization code flow | 243ms | 5 | -10.930 |
| distributed cache eviction policy | 546ms | 5 | +5.679 |
| chunking strategies for embeddings | 307ms | 5 | +6.487 |
| api design best practices | 228ms | 5 | -6.223 |
| compiler tokenization process | 232ms | 5 | -0.341 |
| database indexing b-tree | 323ms | 5 | +7.005 |

**Search-only avg: ~285ms**

---

## Generate (3 queries, warm GPU)

| Query | Wall clock | LLM output |
|-------|-----------|------------|
| how does consistent hashing work | 2,189ms | Correct — described hash ring, virtual nodes, load imbalance |
| rate limiting algorithms | 1,011ms | Partial — returned defense-in-depth strategy, not specific algorithms |
| database indexing b-tree | 1,001ms | Partial — conflated B-tree with ID generation |

**Generate avg (warm): ~1,400ms**
**Generate avg (cold model load): ~3,200ms** (first call after restart)

---

## Prometheus Metrics Snapshot (15 search + 5 generate requests)

Collected via `GET /metrics` after test run.

### Retrieve (Qdrant hybrid search)

| Metric | Value |
|--------|-------|
| Count | 15 |
| Sum | 1.688s |
| **Avg** | **113ms** |
| p60 (bucket ≤0.025) | 9/15 queries under 25ms |
| p80 (bucket ≤0.05) | 12/15 queries under 50ms |
| Max observed | ~944ms (cold Qdrant on first post-restart query) |

### Rerank (cross-encoder sidecar)

| Metric | Value |
|--------|-------|
| Count | 15 |
| Sum | 3.408s |
| **Avg** | **227ms** |
| p73 (bucket ≤0.25) | 11/15 under 250ms |
| p100 (bucket ≤0.5) | all 15 under 500ms |
| Score delta avg | +1.85 (reranker improves top-1 consistently) |
| Score delta p33 | ≤ -0.5 (4/15 queries: reranker found worse top-1 — low-coverage topics) |

### Search End-to-End (embed + retrieve + rerank)

| Metric | Value |
|--------|-------|
| Count | 15 |
| Sum | 5.329s |
| **Avg** | **355ms** |
| p47 (bucket ≤0.25) | 7/15 under 250ms |
| p87 (bucket ≤0.5) | 13/15 under 500ms |
| p93 (bucket ≤1.0) | 14/15 under 1s |

### Generate (LLM streaming)

| Metric | Value |
|--------|-------|
| Count | 5 |
| Sum total | 5.537s |
| **Avg total** | **1,107ms** |
| Sum TTFT | 3.431s |
| **Avg TTFT** | **686ms** |
| TTFT p60 (≤0.5s) | 3/5 under 500ms |
| TTFT p100 (≤2s) | all 5 under 2s |
| Gen ≤1s | 3/5 |
| Gen ≤2s | 5/5 (all under 2s) |

### Cache

| Metric | Value |
|--------|-------|
| Cache hits | 0 |
| Cache misses | 10 |
| Hit rate | 0% (all unique queries, no repeats in test) |

---

## Throughput Estimate (single node)

| Mode | Estimated RPS | Bottleneck |
|------|--------------|------------|
| Search-only (hybrid+rerank) | ~3–4 RPS | Reranker sidecar (serial Python) |
| Generate (warm model) | ~0.7 RPS | Ollama serial queue |
| Cache hit | ~15–20 RPS | Qdrant lookup only |

**Suitable for:** personal RAG, internal team tool (≤20 concurrent users).
**Not suitable for:** product with >50 concurrent users without horizontal Ollama scaling.

---

## k6 Load Tests

Scripts in `tests/k6/`. k6 v0.55.1 at `~/.local/bin/k6`.

**VU = Virtual User** — concurrent simulated client. Each VU loops independently: send request → wait → repeat. 10 VU = 10 simultaneous in-flight request chains.

### Test Inventory

| Script | Endpoint | VU Profile | Duration | Input Data | Pass Criteria |
|--------|----------|-----------|----------|------------|---------------|
| `smoke.js` | `POST /search` | 1 VU flat | 30s | `testdata/queries.json` → `general` (random) | p50<500ms, p95<2000ms, error<1% |
| `load.js` | `POST /search` | ramp 1→10→25→50→0 | ~6m | `testdata/queries.json` → `general` (random) | p95<5000ms, error<5% |
| `cache_hit_rate.js` | `POST /search` | 10 VU flat | 90s | `testdata/queries.json` → `cache_fixed` (5 fixed queries) | cache_hit_rate>50% |
| `keyword_baseline.js` | `POST /search` | 5 VU flat | 60s | `testdata/queries.json` → `general` (random) | keyword p95<1000ms |
| `ollama_embed_throughput.js` | `POST /api/embeddings` (Ollama direct) | ramp 1→4→8→1 | ~2m | hardcoded short text | no errors at 8 VU |

### Test Data

Queries live in `tests/k6/testdata/queries.json`. Two sets:

- **`general`** (20 queries) — domain-specific, matches KB content (Go, distributed systems, algorithms, databases). Used by smoke/load/keyword tests. Rotate to prevent cache saturation skewing latency.
- **`cache_fixed`** (5 queries) — small fixed set intentionally repeated to warm semantic cache. Used only by `cache_hit_rate.js`.

**Update queries when KB content changes** — generic queries ("how does authentication work") produce empty results and inflate miss latency.

Loaded via `SharedArray` in init context → single parse shared across all VUs (no per-VU copy overhead).

### System Under Test

Services required for search path tests:

| Service | Address | Role in test |
|---------|---------|-------------|
| nadir app | `localhost:8080` | primary endpoint |
| Qdrant | `localhost:6334` (gRPC) | vector store |
| Ollama | `localhost:11434` | embedder (`nomic-embed-text`, 768-dim) |
| Reranker | `localhost:5002` | cross-encoder scoring (skipped if `rerank:false`) |
| SPLADE | `localhost:5001` | sparse scoring (optional, TF fallback) |

### Search Config for Load Tests

```yaml
# config at test time (affects what the pipeline does per request)
generator:
  enabled: false          # generate:false in all load test payloads
reranker:
  candidate_mul: 2
qdrant:
  top_k: 5
  prefetch_mul: 5
semantic_cache:
  enabled: true
  threshold: 0.90
  ttl: 24h
hyde:
  enabled: false
chunk_filter:
  enabled: false
```

### Run

```bash
# single test
k6 run tests/k6/smoke.js

# custom base URL
BASE_URL=http://localhost:8080 k6 run tests/k6/load.js

# Ollama direct (embed throughput only)
OLLAMA_URL=http://localhost:11434 k6 run tests/k6/ollama_embed_throughput.js

# save metrics for analysis
k6 run --out json=results.json tests/k6/smoke.js
```

---

## k6 Load Tests (2026-05-02)

**Config at test time:** hybrid+rerank, `top_k=5`, `generate:false` (search only), semantic cache enabled (`threshold=0.90`), HyDE disabled, chunk_filter disabled.

**Endpoint under test:** `POST /search` (all tests except Ollama embed throughput).

---

### Smoke Test — `tests/k6/smoke.js`

**Purpose:** Sanity check. 1 VU, 30s. Confirms server up, responses valid, no obvious latency regression.

**Parameters:** 1 VU, 30s duration, 1s sleep between requests (→ ~1 req/s per VU).

| Metric | Value |
|--------|-------|
| VUs | 1 |
| Total requests | 27 |
| **RPS** | **0.88 req/s** |
| p50 latency | 32ms |
| p95 latency | 386ms |
| Max latency | 557ms |
| Error rate | 0% |

**Interpretation:** p50 fast (cache hits + warm Qdrant). p95 spike to 386ms = cache miss → full embed+retrieve+rerank. 0% errors = server healthy.

---

### Load Test — `tests/k6/load.js`

**Purpose:** Find saturation point. Ramp VUs 1→10→25→50 over 6 min. All hitting `POST /search` with rotating queries and 0.5s sleep between iterations.

**Parameters:** 4 stages — 1m→10VU, 2m→25VU, 2m→50VU, 1m→0VU. 0.5s sleep per iteration.

| Metric | Value |
|--------|-------|
| Peak VUs | 50 |
| Total requests | 15,373 |
| **RPS (sustained, 50 VU)** | **42.6 req/s** |
| p50 latency | 34ms |
| p95 latency | 83ms |
| Max latency | 185ms |
| Error rate | 0% |

**Interpretation:** 42.6 RPS is `POST /search` (hybrid+rerank, no generate) at 50 VU with 0.5s sleep. p95=83ms — significantly better than smoke p95=386ms because cache warmed fast under concurrent load (5 query slots × 10 VUs → repeated queries). No saturation observed — p95 stayed flat even at 50 VU. Real bottleneck under cache-miss load is reranker sidecar (serial Python process, ~227ms avg per [Prometheus snapshot above](#rerank-cross-encoder-sidecar)).

---

### Cache Hit Rate Test — `tests/k6/cache_hit_rate.js`

**Purpose:** Measure semantic cache effectiveness. Fixed 5-query set repeated at high concurrency → cache saturates fast. Measures hit ratio and latency delta (cached vs uncached).

**Parameters:** 10 VU, 90s, 5 fixed queries (no sleep — tight loop).

| Metric | Value |
|--------|-------|
| VUs | 10 |
| Total requests | 23,842 |
| **RPS** | **264.8 req/s** |
| Cache hits | 23,241 |
| Cache misses | 601 |
| **Hit rate** | **97.47%** |
| Cached p95 latency | 43ms |
| Uncached p95 latency | 93ms |
| Cached avg latency | 37ms |
| Uncached avg latency | 64ms |

**Interpretation:** 264.8 RPS is `POST /search` with warm cache (5 fixed queries, 10 VU). Cache hit = Qdrant cosine lookup only, skips embed+retrieve+rerank. Latency delta: cached 37ms vs uncached 64ms — ~1.7× speedup. Hit detection uses `X-Cache: HIT` header + <50ms heuristic.

---

### Ollama Embed Throughput — `tests/k6/ollama_embed_throughput.js`

**Purpose:** Isolate Ollama `/api/embeddings` capacity. Ramp 1→8 VU to find where serial queue saturates. Uses `nomic-embed-text` model, same as production path.

**Endpoint:** `POST http://localhost:11434/api/embeddings` (Ollama direct, not app server).

**Parameters:** 4 stages — 30s@1VU, 30s@4VU, 30s@8VU, 30s@1VU. No sleep.

| Metric | Value |
|--------|-------|
| Peak VUs | 8 |
| Total requests | 22,054 |
| **RPS (peak, 8 VU)** | **183.8 req/s** |
| Avg latency | 17.6ms |
| p95 latency | 24.6ms |
| Max latency | 56ms |
| Error rate | 0% |

**Interpretation:** Ollama `nomic-embed-text` handles 183 RPS at 8 VU with no errors and stable latency (avg 17.6ms, p95 24.6ms). No saturation — GPU offload keeps embed fast. This means embed is **not** the bottleneck in the search path; reranker sidecar (~227ms avg) is. Search path adds embed (~18ms) + Qdrant retrieval + rerank (~227ms) = ~285ms total observed above.