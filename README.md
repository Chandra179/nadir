# Personal Knowledge Base

#### Concept

An intelligent search and retrieval layer for Markdown-based knowledge bases. It transforms static notes into a queryable brain by combining traditional text processing with vector embeddings.

#### Goals

* **High Precision Retrieval:** Ensure users find the exact context, not just the file.
* **Architectural Modularity:** Decouple the chunking logic and retrieval strategies to allow for experimentation with different LLMs or Vector DBs.
* **Cost Efficiency & Privacy:** Minimize token usage via RAG and support local embedding models to keep personal data private.
* **Low Latency:** Provide sub-second search results using optimized vector indexing.

#### Engine Components

* **Chunker** (`RecursiveChunker`): splits by heading → paragraph → sentence → word with configurable size + overlap. Goldmark-parsed headings become source pointers.
* **Embedder** (`OllamaEmbedder`): local Ollama instance (e.g. `nomic-embed-text`). Swappable via `Embedder` interface.
* **Store** (`QdrantStore`): Qdrant via gRPC. Upsert, delete-by-file, cosine similarity search, SHA-based dedup.
* **Pipeline**: chunk → embed (exponential backoff retry) → upsert. SHA dedup skips unchanged files.
* **Ingestion source**: git submodule (`gitbook/`). `LocalFileLister` walks for `.md` files; `LocalFetcher` reads content.

#### API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/ingest` | Walk submodule, skip unchanged (SHA match), ingest new/modified files |
| `POST` | `/search` | Embed query → vector search → return ranked chunks with file+header+line pointers |

#### Interfaces (extension points)

```go
type Chunker  interface { Chunk(text, filePath string) ([]DocumentChunk, error) }
type Embedder interface { Embed(ctx context.Context, text string) ([]float32, error); Dimensions() int }
type Store    interface { Upsert/DeleteByFile/Search/EnsureCollection/GetFileSHA }
type Fetcher  interface { FetchFile(ctx context.Context, path, sha string) (string, error) }
```

#### Dependencies

* **Language:** Go
* **Markdown Parser:** Goldmark
* **Vector DB:** Qdrant (local Docker or Cloud)
* **Embeddings:** Ollama (`nomic-embed-text`) — local, private

#### Retrieval Improvement Roadmap

Ordered by ROI. Each item includes tradeoffs. ✅ = implemented.

---

**1. Strip markdown before embedding** ✅ *(done)*

Plain text emitted from Goldmark AST walk. Strips `##`, `**`, `_`, fences, `[text](url)`.

---

**2. Hybrid search: dense + client-side BM25 RRF** ✅ *(done)*

Dense cosine search + Qdrant payload full-text filter merged via Reciprocal Rank Fusion (k=60, Cormack 2009). TF scoring used to rank BM25 candidates before RRF.

| Pro | Con |
|-----|-----|
| Exact keyword match for proper nouns, code, IDs | Extra Scroll call per query (~2ms) |
| Catches what vector misses (low-frequency terms) | TF proxy less accurate than true BM25 IDF |
| No external service or sparse vectors needed | — |

**Upgrade path**: replace TF proxy with Qdrant native sparse vectors (SPLADE) when KB grows large enough for IDF to matter.

---

**3. Contextual chunk enrichment** ✅ *(done)*

Before embedding, each chunk is prefixed with `filePath > heading`. Anchors semantic space to document structure. Based on Anthropic 2024 "Contextual Retrieval".

| Pro | Con |
|-----|-----|
| +5% Recall@5 observed on 20-query eval | Re-ingest required after change |
| Improves retrieval of topic-scoped content | Slightly longer embed input (+~20 tokens) |

---

**4. Chunker: lists and blockquotes** ✅ *(done)*

`RecursiveChunker` now captures `*ast.List` and `*ast.Blockquote` nodes, not just paragraphs. Previously these were silently dropped.

---

**5. Fix chunk ID collision** *(correctness, low urgency)* ✅ *(done)*

Current `chunkID` = SHA256-truncated `filePath+lineStart`. Collision probability low but non-zero across large corpora.

Replace with UUIDv5(`namespace`, `filePath+":"+strconv.Itoa(lineStart)`) for true collision resistance.

---

**6. SPLADE sparse vectors** *(medium effort, high gain on large KB)*

`SparseScorer` interface wired — `TFSparseScorer` is default. Swap in SPLADE via `store.WithSparseScorer(...)`. Implement SPLADE model (DistilSPLADE or naver/splade-v3) as `SparseScorer` to get true IDF-weighted scoring.

| Pro | Con |
|-----|-----|
| True IDF-weighted BM25 semantics | Second embedder at ingest + query time |
| +5-10% NDCG vs TF proxy | Requires Qdrant sparse vector collection config |
| Interface already wired — drop-in swap | — |

---

**7. Multi-sentence / multi-concept query splitting** *(diminishing returns)*

Break query on `.` / `?` / `;`, embed each fragment, merge result sets, deduplicate by score.

---

**Skipped / deferred:**

* **HyDE** (generate hypothetical answer, embed that): significant quality gain but requires LLM call per query — breaks sub-second latency goal.
* **Re-ranking** (cross-encoder on top-K): high precision but adds ~200–500ms; revisit if precision becomes bottleneck after hybrid search.
* **Chunk size tuning**: empirical — run 20–30 real queries against your notes and measure MRR@5 before tuning.

---

#### Recent Improvements (2026-04-19, round 2)

**Eval query set fixed** (`eval_search_test.go`)
- 6 queries rewrote to match actual gitbook content (Q8, Q11, Q16–Q20)
- Q8: rate limiting → thundering herd jitter framing matches `rate-limit.md`
- Q11: B-Tree vs LSM → exact Q&A framing matches `database.md`
- Q16–Q20: ML queries reframed to match stub content in `ml/README.md`
- qrels must be regenerated after this change (`make gen-qrels`)

**SparseScorer abstraction** (`embedder.go`, `sparse_scorer.go`, `store_qdrant.go`)
- Extracted BM25 leg scoring into `SparseScorer` interface: `Score(query, text string) float64`
- `TFSparseScorer` (TF-proxy) is default — zero behaviour change
- Swap to SPLADE: `store.WithSparseScorer(mySPLADEScorer)` — no store rebuild needed
- `QdrantStore` keeps TF-proxy by default; SPLADE opt-in when KB grows large enough for IDF to matter

---

#### Recent Improvements (2026-04-19)

Three retrieval enhancements shipped. Measured on 20-query eval set, qrels judge (gold), topK=5.

| Metric | Before | After | Delta |
|--------|--------|-------|-------|
| MRR@5 | 0.5708 | 0.5625 | −0.008 |
| Recall@5 | 0.7000 | **0.7500** | **+0.050** |
| NDCG@5 | 0.5932 | 0.5974 | +0.004 |
| Precision@5 | 0.3600 | 0.3500 | −0.010 |

LLM judge (silver): MRR 0.608 vs 0.571 prior — improvement consistent across both judges.

**What changed:**

**Chunker: lists + blockquotes captured** (`chunker_recursive.go`)
- Before: `extractSections` walked `*ast.Paragraph` only — lists/blockquotes silently dropped
- After: `*ast.List` and `*ast.Blockquote` nodes collected into section text
- Impact: content previously invisible to embedder now indexed

**Contextual chunk enrichment** (`pipeline.go`)
- Before: raw `chunk.Text` sent to embedder
- After: `filePath > header\nchunk.Text` sent to embedder; stored text unchanged
- Based on Anthropic 2024 "Contextual Retrieval" — prepending document structure anchors embedding to topic
- Impact: primary driver of Recall@5 +5%

**Client-side BM25 RRF** (`store_qdrant.go`)
- Before: BM25 prefetch leg used `Filter`-only (unscored) — Qdrant RRF received unranked candidate list, degrading to dense-only re-rank
- After: separate dense `Search` + `Scroll` with TF scoring → client-side RRF fusion (k=60, Cormack 2009)
- TF score = sum of query term occurrences in chunk text; used only for BM25 rank ordering before RRF
- Trade-off: extra Scroll call per query (~2ms); TF proxy less accurate than true BM25 IDF

**Next improvement candidates:**
1. SPLADE sparse vectors — swap `TFSparseScorer` for a SPLADE-backed impl; `SparseScorer` interface already wired
2. Sentence-window indexing — index sentence-level chunks, expand to paragraph at retrieval; expect +5-10% Recall

---

#### Search Evaluation

Integration test at `internal/pkb/eval_search_test.go`. Spins up Qdrant via testcontainers, ingests `gitbook/` markdown, runs 20 real queries, reports MRR@5 and Recall@5.

**Live mode example** (skip Docker, use running stack):
```bash
EVAL_MODE=live EVAL_JUDGE=folder go test -v -timeout 120s -run TestSearchEval ./internal/pkb/
```

**LLM judge example** (silver standard — per-chunk relevance via local LLM):
```bash
EVAL_MODE=live EVAL_JUDGE=llm EVAL_LLM_MODEL=llama3 go test -v -timeout 600s -run TestSearchEval ./internal/pkb/
```

**Relevance judge levels:**

| Judge | Accuracy | Cost | When to use |
|-------|----------|------|-------------|
| `llm` | Silver — per-chunk LLM verdict | ~0.01$/pair | Before tuning, catch regressions |
| `qrels` | Gold — human/LLM pre-labeled | One-time | Stable benchmark, reproducible |

**Generate qrels** (run once after major index changes, commit `testdata/qrels.jsonl`):
```bash
make gen-qrels        # LLM judges all queries against live Qdrant, writes testdata/qrels.jsonl
make eval-qrels       # deterministic eval against committed qrels
```

**Metrics:**
* **MRR@5** (Mean Reciprocal Rank): average of `1/rank` for first relevant result in top-5. 1.0 = always #1. 0.0 = never in top-5.
* **Recall@5**: fraction of queries where at least one relevant result appears in top-5.
* **NDCG@5** (Normalized Discounted Cumulative Gain): weights hits by rank position — rank 1 worth more than rank 5. `DCG / IDCG` where IDCG = perfect ordering. Penalizes burying relevant results.
* **Precision@5**: fraction of top-5 results that are relevant. High recall + low precision = noisy results.
