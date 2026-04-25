# Personal Knowledge Base

#### Concept

Intelligent search and retrieval layer for Markdown-based knowledge bases. Transforms static notes into a queryable brain via dense vector search, sparse lexical search (SPLADE), and RRF fusion.

#### Goals

* **High Precision Retrieval:** Find exact context, not just the file.
* **Architectural Modularity:** Decouple chunking and retrieval strategies for experimentation with different LLMs or vector DBs.
* **Cost Efficiency & Privacy:** Minimize token usage via RAG; local embedding models keep data private.
* **Low Latency:** Sub-second search via optimized vector indexing.

---

#### Engine Components

* **Chunker** (`RecursiveChunker`): Goldmark AST walk → splits by heading → paragraph → sentence → word. Configurable size + overlap. Lists and blockquotes captured. Plain text emitted (strips `##`, `**`, `_`, fences, links).
* **Embedder** (`OllamaEmbedder`): local Ollama (`nomic-embed-text`, 768-dim). Swappable via `Embedder` interface.
* **Sparse Scorer** (`TFSparseScorer` / `SPLADESparseScorer`): client-side BM25 leg. TF-proxy by default; SPLADE opt-in for true IDF semantics.
* **Store** (`QdrantStore`): Qdrant via gRPC. Upsert, delete-by-file, cosine similarity search, SHA-based dedup. Hybrid search: dense + sparse via RRF (server-side QueryPoints or client-side scroll+merge).
* **Pipeline**: chunk → contextual prefix → embed (exponential backoff retry) → sparse embed (optional) → upsert. SHA dedup skips unchanged files.
* **Ingestion source**: `LocalFileLister` walks `knowledge_base.path` for `.md` files; `LocalFetcher` reads content.

#### API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/ingest` | Walk KB dir, skip unchanged (SHA match), ingest new/modified files |
| `POST` | `/search` | Embed query → hybrid search → ranked chunks with file+header+line pointers |

**Search request:**
```json
{ "query": "...", "top_k": 5 }
```
Keyword-only mode: `{ "keyword": "..." }`. Multi-sentence queries auto-split on `.`/`?`/`;`, results merged by best score.

#### Interfaces (extension points)

```go
type Chunker       interface { Chunk(text, filePath string) ([]DocumentChunk, error) }
type Embedder      interface { Embed(ctx context.Context, text string) ([]float32, error); Dimensions() int }
type SparseEmbedder interface { EmbedSparse(ctx, text, mode) (indices []uint32, values []float32, err error) }
type SparseScorer  interface { Score(query, text string) float64 }
type Store         interface { Upsert / DeleteByFile / Search / HybridSearch / KeywordSearch / EnsureCollection / GetFileSHA }
type Fetcher       interface { FetchFile(ctx context.Context, path, sha string) (string, error) }
type FileLister    interface { ListMarkdownFiles(ctx, rootDir, sha string) ([]FileEntry, error) }
```

#### Dependencies

* **Language:** Go + Python sidecar (SPLADE only)
* **Markdown Parser:** Goldmark
* **Vector DB:** Qdrant (local Docker or Cloud)
* **Embeddings:** Ollama (`nomic-embed-text`) — local, private
* **Sparse Embeddings:** `fastembed` + `Qdrant/splade-v3-distilbert` (optional)

---

#### Search Evaluation

Integration test at `internal/pkb/eval_search_test.go`. Runs 20 real queries against ingested markdown, judges relevance, reports MRR@5 / Recall@5 / NDCG@5 / Precision@5.

See `internal/pkb/EVAL.md` for full decision tree.

| Target | When to use | Prereqs |
|--------|-------------|---------|
| `make eval-tf` | Fast daily feedback, TF sparse | `make up && make ingest` |
| `make eval-splade` | Compare TF vs SPLADE on live Qdrant | `make splade` + above |
| `make eval-fresh` | Self-contained CI, full re-ingest | none (pulls Docker) |
| `make eval-llm` | LLM judges live, no qrels needed | `make up && make ingest` |

**Key env vars:**

| Variable | Default | Purpose |
|----------|---------|---------|
| `EVAL_STORE` | `live` | `live` or `container` Qdrant backend |
| `EVAL_JUDGE` | `qrels` | `qrels` (gold) or `llm` (silver) |
| `EVAL_QDRANT_ADDR` | from config | Override Qdrant gRPC addr |

**Generate qrels** (after major index changes):
```bash
make eval-tf    # runs gen-qrels then deterministic eval
```

**Metrics:**
* **MRR@5**: `mean(1/rank)` for first relevant result in top-5. 1.0 = always #1.
* **Recall@5**: fraction of queries with ≥1 relevant result in top-5.
* **NDCG@5**: rank-discounted gain. `DCG / IDCG`. Penalizes burying relevant results.
* **Precision@5**: fraction of top-5 that are relevant. High recall + low precision = noisy.

---

#### Retrieval Roadmap

Ordered by ROI. ✅ = implemented.

**Baseline (2026-04-19):** MRR@5=0.60 · Recall@5=0.60 · NDCG@5=0.583 · Precision@5=0.32 · 20 queries · SPLADE+nomic-embed-text · chunk=512/64

Recall@5=0.60 is the binding constraint — 40% of queries return nothing relevant in top-5. Fix recall before chasing precision.

| # | Feature | Status | Notes |
|---|---------|--------|-------|
| 1 | Strip markdown before embedding | ✅ | Goldmark AST → plain text |
| 2 | Hybrid search: dense + BM25 RRF | ✅ | k=60, Cormack 2009; TF proxy by default |
| 3 | Contextual chunk enrichment | ✅ | `filePath > heading\nchunk`; +5% Recall@5 |
| 4 | Chunker: lists + blockquotes | ✅ | Previously silently dropped |
| 5 | SPLADE sparse vectors | ✅ | Python sidecar; true IDF; +5-10% NDCG |
| 6 | Multi-sentence query splitting | ✅ | Split on `.`/`?`/`;`, merge by best score |
| 7 | Fix chunk ID collision | ✅ | Replace FNV hash with UUIDv5; correctness prerequisite for sentence-window |
| 8 | Sentence-window indexing | ✅ | Index sentences, expand to paragraph at retrieval; expected +5-10% Recall@5 |
| 9 | Expand eval set (20 → 50+ queries) | ✅ | 20 queries = high variance; small deltas are noise; need more signal before chunk ablation |
| 10 | Chunk size ablation (512 → 256, try 128) | ✅ | Profiles: tf/splade × 512/256/128; overlap scales proportionally (12.5%); run `make eval-fresh` |
| 11 | Payload index on `file_path` | ✅ | Keyword index created in `EnsureCollection`; eliminates full-scan in `GetFileSHA` + `DeleteByFile` |
| 12 | Re-ranking (cross-encoder) | ✅ | `cross-encoder/ms-marco-MiniLM-L-6-v2` Python sidecar; `candidate_mul=3` oversampling; enable via `reranker.enabled: true` + `python cmd/reranker/main.py` |
| 13 | HyDE | pending | LLM call per query breaks latency goal |
| 14 | Semantic cache | pending | Embed query → vector search cache at 0.85–0.95 threshold → return cached result; up to 68.8% LLM call reduction, 65× faster hits; new `SemanticCache` interface wrapping `Store.Search` |
| 15 | Batch embedding API | pending | `Embedder` is single-text; Ollama `/api/embed` accepts arrays — batch cuts HTTP round-trips from O(chunks) to O(1) per file; add `BatchEmbedder` interface |
| 16 | Observability / metrics | pending | Runtime counters: cache hit rate, retrieval precision, rerank delta, embedding latency; instrument via `expvar` or Prometheus; blind in prod without this |
| 17 | Rate limiting (multi-level) | pending | User/tenant + LLM API + vector DB + system tiers; stdlib `golang.org/x/time/rate` sufficient for single-node; needed before public exposure |
| 18 | Bulk SHA check at ingest | ✅ | `Store.GetAllFileSHAs()` added; single paginated scroll replaces O(N) RPCs |
| 19 | Concurrent dense+sparse embed | ✅ | Dense + sparse embed run concurrently per chunk via `sync.WaitGroup`; per-file ingestion still sequential |

**Enable SPLADE:** set `sparse_scorer.provider: splade` in `config/config.yaml`, then run `python cmd/splade/main.py`.

**Enable re-ranking:** set `reranker.enabled: true` in `config/config.yaml`, then run `python cmd/reranker/main.py` (`pip install sentence-transformers fastapi uvicorn`). Fetches `topK * candidate_mul` candidates from hybrid search, scores all with `cross-encoder/ms-marco-MiniLM-L-6-v2`, returns top-k. Adds ~100-400ms on CPU.

---

#### Technical Debt & Optimization Backlog

##### Performance
* **Sequential per-file ingestion**: bounded-concurrency parallel ingest via `errgroup` + semaphore; ingest time ∝ 1/workers. `fileContentSHA` also reads each file twice (once for SHA in lister, once in fetcher) — merge if moving to concurrent path.
* **No batch embedding API**: `Embedder` is single-text; Ollama `/api/embed` accepts arrays. Batch cuts HTTP round-trips from O(chunks) to O(1) per file; add `BatchEmbedder` interface.

##### Maintainability & Structure
* **`EvalConfig` in production `Config`**: eval/LLM judge settings ship in production config struct. Move to separate `EvalConfig` loaded only by eval harness.
* **`EnsureCollection` in `Store` interface**: mixes lifecycle with query ops. Split into `StoreAdmin` so handlers only receive query capability.
* **`IngestHandler.ServeHTTP` contains full ingest loop**: fetch + SHA-check + pipeline logic belongs in a service method, not the HTTP handler.
* **`runEval` / `evalMetrics` in `_test.go`**: eval is first-class concern. Move to `cmd/eval` binary, callable without `go test`.
* **No `Config.Validate()`**: zero `Qdrant.TopK`, empty `Embedder.Model`, zero dimensions silently produce broken runtime. Add validation.
* **`QdrantStore` has no `Close()`**: gRPC `conn` not stored — constructor must retain it. Fine for process-lifetime but blocks multi-store testing.