# Personal Knowledge Base

This repo is an example for golang template project, modules name, functionality is just an example

## Architectural Definitions
* **Component (The App):** This entire repository is a single Component. It is an independently deployable unit that provides a set of related business capabilities.
* **Modules (Internal Logic):** Located in `internal/modules/`, these are logical wrappers (Go packages) used to maintain high **Functional Cohesion**. 

## Why this Structure?
1.  **Modularity:** Logic is partitioned by domain (`order`, `calc`) rather than technical layers.
2.  **Fitness Functions:** This structure allows you to write tests (e.g., using `ArchGuard` or `go-cyclomatic`) to ensure the `calc` module doesn't accidentally start importing `httpserver` logic.
3.  **Evolutionary Path:** If the `calc` module's architecture characteristics change (e.g., it needs massive scalability), it is decoupled enough to be extracted into a separate **Architecture Quantum**.

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

Ordered by ROI. Each item includes tradeoffs.

---

**1. Strip markdown before embedding** *(do first — easy win)*

Emit plain text from AST walk instead of raw markdown. Drop `##`, `**`, `_`, fences, `[text](url)`.

| Pro | Con |
|-----|-----|
| Embedding model sees clean semantic signal | Lose inline code formatting cues (minor) |
| No dilution from syntax noise | Need to update chunker AST output |
| Improves cosine similarity accuracy | — |

---

**2. Hybrid search: BM25 + vector** *(highest recall improvement)*

Add Qdrant sparse vector index (or payload full-text index) alongside dense cosine search. Merge scores (Reciprocal Rank Fusion or weighted sum).

| Pro | Con |
|-----|-----|
| Exact keyword match for proper nouns, code, IDs | Qdrant sparse vectors require reindex |
| Catches what vector misses (low-frequency terms) | Score fusion adds tuning surface |
| No external service needed — Qdrant native | Slightly higher query latency |

---

**3. Keyword filter endpoint** *(cheap debug + power-user path)*

Add `POST /search` optional `keyword` field → Qdrant payload full-text filter (requires text index on `text` field).

| Pro | Con |
|-----|-----|
| Deterministic, inspectable results | No semantic understanding |
| Useful for debugging retrieval gaps | Requires creating payload index in Qdrant |
| Zero extra latency | — |

---

**4. Fix chunk ID collision** *(correctness, low urgency)*

Current `chunkID` = FNV hash of `filePath+lineStart`. Two different files can collide.

Replace with UUIDv5(`namespace`, `filePath+":"+strconv.Itoa(lineStart)`) or SHA256-truncated-to-uint64.

| Pro | Con |
|-----|-----|
| Eliminates silent overwrites | Requires re-ingest to regenerate IDs |
| Deterministic and collision-resistant | — |

---

**5. Multi-sentence / multi-concept query splitting** *(diminishing returns)*

Break query on `.` / `?` / `;`, embed each fragment, merge result sets, deduplicate by score.

| Pro | Con |
|-----|-----|
| Better recall for compound questions | 2–3× embed calls per query |
| Surfaces results for each sub-concept | Complexity: need merge + dedup logic |
| — | Marginal gain for single-concept queries (majority of PKB searches) |

---

**Skipped / deferred:**

* **HyDE** (generate hypothetical answer, embed that): significant quality gain but requires LLM call per query — breaks sub-second latency goal.
* **Re-ranking** (cross-encoder on top-K): high precision but adds ~200–500ms; revisit if precision becomes bottleneck after hybrid search.
* **Chunk size tuning**: empirical — run 20–30 real queries against your notes and measure MRR@5 before tuning.