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
{"name": "tf-recursive256-multi-hyde3", "tags": ["tf","hyde","multi-hyde"], "sparse_scorer": "tf", "chunk_size": 256, "chunk_overlap": 32, "multi_hyde": true, "hyde_num_docs": 3}
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

## System Snapshot (2026-05-01)

### Unit Tests

```
go test ./internal/pkb/ -run 'Test[^S]'
ok  nadir/internal/pkb  0.023s
```

All non-eval unit tests pass.

### Eval Tests

```
go test ./internal/pkb/ -run TestSearchEval -v -timeout 10m
ok  nadir/internal/pkb  429.434s
```

All 6 profiles pass. Runtime dominated by HyDE LLM generation (~7 min for HyDE profiles, ~5s for baseline).

### Corpus Stats

| Metric | Value |
|--------|-------|
| Vectors stored (Qdrant) | 554 |
| Chunk size | 256 tokens |
| Chunk overlap | 32 tokens |
| Chunker | recursive |
| Eval queries | 50 |
| Qrels total | 500 |

### Eval Results (qrels judge, top-K=5)

| Profile | MRR | Hit@5 | NDCG@5 | P@5 |
|---------|-----|-------|--------|-----|
| tf-recursive256-baseline | 0.918 | 1.000 | 0.941 | 0.840 |
| tf-recursive256-hyde1 | 0.916 | 1.000 | 0.927 | — |
| tf-recursive256-multi-hyde3 | 0.903 | 0.980 | 0.914 | 0.688 |
| tf-recursive256-multi-hyde5 | 0.916 | 1.000 | 0.927 | 0.688 |
| tf-recursive256-adaptive-hyde | 0.892 | 0.960 | 0.904 | 0.708 |
| splade-recursive256-adaptive-hyde | **0.950** | **1.000** | **0.955** | **0.836** |

Best: `splade-recursive256-adaptive-hyde` — highest MRR + NDCG. SPLADE sparse + adaptive HyDE outperforms TF baseline across all metrics.

### Category Breakdown (splade-recursive256-adaptive-hyde)

| Category | N | NDCG@5 | Hit@5 |
|----------|---|--------|-------|
| rag | 2 | 1.0000 | 1.0000 |
| databases | 5 | 0.9758 | 1.0000 |
| computer-science | 5 | 0.9234 | 1.0000 |
| golang | 11 | 0.8663 | 1.0000 |
| networking | 1 | 0.6183 | 1.0000 |
| system-design | 17 | 0.8566 | 0.9412 |
| math | 9 | 0.8245 | 0.8889 |

### Misses (2/50)

1. **[system-design/medium]** "What is the Snowflake ID structure and how does it handle clock skew?" — top1: `system-design/consistent-hashing.md:18` (score=0.016)
2. **[math/hard]** "What is QR decomposition and when is it used over LU?" — top1: `math/linear-algebra.md:290` (score=0.015)

Both misses have very low scores (≤0.016), indicating missing or insufficient coverage in the corpus, not retrieval failure.

### Sample Search

Server must be running (`make run`) to execute `make search`. Example query: `{"query":"personal knowledge base","top_k":10}`.

---