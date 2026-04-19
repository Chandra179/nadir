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
| 7 | Fix chunk ID collision | **next** | Replace FNV hash with UUIDv5; correctness prerequisite for sentence-window |
| 8 | Sentence-window indexing | **next** | Index sentences, expand to paragraph at retrieval; expected +5-10% Recall@5 |
| 9 | Expand eval set (20 → 50+ queries) | **next** | 20 queries = high variance; small deltas are noise; need more signal before chunk ablation |
| 10 | Chunk size ablation (512 → 256, try 128) | pending | Smaller chunks → more precise hits → recall+precision; run after eval set expanded |
| 11 | Payload index on `file_path` | pending | Currently full collection scan on every SHA check; fast perf win from debt list |
| 12 | Re-ranking (cross-encoder) | deferred | +precision but +200-500ms; revisit after recall ≥ 0.75 |
| 13 | HyDE | deferred | LLM call per query breaks latency goal |

**Enable SPLADE:** set `sparse_scorer.provider: splade` in `config/config.yaml`, then run `python cmd/splade/main.py`.

---

#### Technical Debt & Optimization Backlog

Issues found during analysis, grouped by impact.

##### Correctness

* **`EnsureCollection` catches all errors from `Get`, not just `NotFound`** (`store_qdrant.go`). A transient network error triggers a spurious `Create` call. Fix: check for `codes.NotFound` gRPC status.
* **`SPLADESparseScorer.Score` uses `context.Background()`** instead of the caller's context — HTTP calls inside are not cancellable. Fix: thread context through `SparseScorer.Score`.
* **SPLADE sidecar failure silently zeros scores** in client-side hybrid search path, degrading to dense-only with no error surfaced to caller.

##### Redundant Code

* **`chunkKey` duplicated three times**: `store_qdrant.go`, `handler_search.go`, `eval_search_test.go` all build `filePath + ":" + strconv.Itoa(lineStart)`. Add `(c DocumentChunk) ID() string` method.
* **Qdrant payload extraction duplicated**: the 5-field extraction (`text`, `file_path`, `header`, `line_start`, `source_sha`) is written in `Search`, `hybridSearchServer`, `hybridSearchClient`, and `KeywordSearch`. Extract `chunkFromPayload(p map[string]*qdrant.Value) ScoredChunk`.
* **`PipelineConfig` == `config.RetryConfig`**: identical four fields copied manually at every call site. Embed or alias one from the other.
* **`EmbedderConfig.Provider` and `ChunkerConfig.Provider`**: populated in config, never dispatched on — no runtime effect.
* **`buildSparseScorer` in eval test**: always returns `TFSparseScorer{}` regardless of arguments. Replace with a direct literal.
* **`FolderJudge` reference** in `eval_judge.go` comment — type does not exist.

##### Performance

* **Sequential SHA-check RPCs during ingest** (`handler_ingest.go`): N files = N Qdrant scrolls. Add a bulk `GetAllFileSHAs() map[string]string` to `Store` interface.
* **Sequential per-file ingestion**: bounded-concurrency parallel ingest via `errgroup` + semaphore would reduce ingest time proportionally.
* **Sequential dense + sparse embedding per chunk** (`pipeline.go`): two calls are independent — run concurrently with `errgroup`.
* **No batch embedding API**: `Embedder` interface is single-text only. Ollama `/api/embed` accepts arrays. Batch would cut HTTP round-trips from O(chunks) to O(1) per file.
* **`file_path` has no Qdrant payload index**: `GetFileSHA` and `DeleteByFile` do full collection scans. Add payload index on `file_path` in `EnsureCollection`.
* **`len([]rune(s))` allocates** in `chunker_recursive.go` — use `utf8.RuneCountInString(s)`.
* **`filepath.Walk` → `filepath.WalkDir`** in `file_lister_local.go` (avoids extra `os.Lstat` per file, available since Go 1.16).
* **`fileContentSHA` reads file twice** — once for SHA, once in `FetchFile`. Compute SHA during fetch.

##### Maintainability & Structure

* **`EvalConfig` in production `Config`**: eval/LLM judge settings (history path, model) ship in the production config struct and file. Move to a separate `EvalConfig` loaded only by the eval harness.
* **`EnsureCollection` in `Store` interface**: mixes lifecycle with query operations. Split into `StoreAdmin` interface so handlers only receive query capability.
* **`SparseScorer` and `Embedder`/`SparseEmbedder` in `embedder.go`**: scoring ≠ embedding. Move `SparseScorer` to `sparse_scorer.go` where its implementations live.
* **`IngestHandler.ServeHTTP` contains full ingest loop**: fetch + SHA-check + pipeline logic belongs in a service method, not the HTTP handler.
* **`multiSearch` and `splitFragments` in `handler_search.go`**: query decomposition strategy embedded in handler — not unit-testable. Extract to a `QueryStrategy` or `search.go` helper.
* **`runEval` / `evalMetrics` in `_test.go`**: eval is a first-class concern per `EVAL.md`. Metrics code should live in a `cmd/eval` binary, callable without `go test`.
* **No `Config.Validate()`**: zero `Qdrant.TopK`, empty `Embedder.Model`, or zero dimensions silently produce a broken runtime. Add validation.
* **`QdrantStore` has no `Close()`**: gRPC connection is never explicitly closed. Fine for process-lifetime use but blocks clean testing with multiple store instances.
* **`LocalFetcher.Root` is a public field** — should be unexported like `LocalFileLister`.
* **`WithSparseScorer` fluent pattern misleads**: mutates in place AND returns `*QdrantStore`; call site in `httpserver/server.go` discards the return value. Either make it a pure setter (no return) or enforce the chaining pattern with documentation.
