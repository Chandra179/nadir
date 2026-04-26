# Personal Knowledge Base

## Quick Start (Local)

**Prerequisites:** Go 1.22+, Docker, Ollama

```bash
# 1. Clone + pull knowledge base (optional submodule)
git clone <repo> && cd nadir
make sm                        # add gitbook submodule (skip if using own notes dir)

# 2. Copy env and configure
cp .env.example .env
# Edit .env: set NOTES_PATH to your markdown directory

# 3. Pull embedding model
ollama pull nomic-embed-text

# 4. Start everything (Qdrant + server + ingest)
make dev                       # scripts/dev-local.sh — up, wait, run, ingest
```

---

## Production Setup

> Full infra guide, resource sizing, and tier-by-tier scaling: [`docs/INFRA_SETUP.md`](docs/INFRA_SETUP.md)
> Tier 1 service diagram: [`diagrams/infra-tier1.md`](diagrams/infra-tier1.md)

**Tier 1 — single box (Hetzner CX52, ~€38/mo, <500 concurrent users):**

```bash
# 1. Copy and configure env
cp .env.example .env
# Set: GRAFANA_PASSWORD, NOTES_PATH, LOGGER_LEVEL=prod

# 2. Deploy + ingest
make prod                      # scripts/prod-start.sh — up-prod, wait, ingest, print cron instructions

# Manual backup triggers:
make snapshot                  # snapshot via Qdrant REST API
make backup                    # volume tar.gz backup
```

**Tier 2+ (separate ML machine):** update `SPLADE_ADDR`, `RERANKER_ADDR`, `OLLAMA_ADDR` in `.env` to ML machine IP. See `docs/INFRA_SETUP.md`.

---

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

#### Dependencies

* **Language:** Go + Python sidecar (SPLADE only)
* **Markdown Parser:** Goldmark
* **Vector DB:** Qdrant (local Docker or Cloud)
* **Embeddings:** Ollama (`nomic-embed-text`) — local, private
* **Sparse Embeddings:** `fastembed` + `Qdrant/splade-v3-distilbert` (optional)

---

#### Search Evaluation

**Metrics:**
* **MRR@5**: `mean(1/rank)` for first relevant result in top-5. 1.0 = always #1.
* **Recall@5**: fraction of queries with ≥1 relevant result in top-5.
* **NDCG@5**: rank-discounted gain. `DCG / IDCG`. Penalizes burying relevant results.
* **Precision@5**: fraction of top-5 that are relevant. High recall + low precision = noisy.

---

#### Roadmap

Ordered by ROI. ✅ = implemented.

**Baseline (2026-04-19):** MRR@5=0.60 · Recall@5=0.60 · NDCG@5=0.583 · Precision@5=0.32 · 20 queries · SPLADE+nomic-embed-text · chunk=512/64

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
| 13 | HyDE | ✅ | Ollama LLM generates hypothetical doc; embed doc instead of raw query; avg N embeddings (L2-norm); falls back to standard search on failure; `num_docs=1` default (fast); `num_docs=8` per paper |
| 14 | Semantic cache | ✅ | Qdrant-backed `SemanticCache`; cosine threshold (default 0.90); lazy TTL expiry; fire-and-forget write; enable via `semantic_cache.enabled: true` |
| 15 | Batch embedding API | ✅ | `Embedder` is single-text; Ollama `/api/embed` accepts arrays — batch cuts HTTP round-trips from O(chunks) to O(1) per file; add `BatchEmbedder` interface |
| 16 | Observability / metrics | ✅ | Runtime counters: cache hit rate, retrieval precision, rerank delta, embedding latency; instrument via `expvar` or Prometheus; blind in prod without this |
| 17 | Rate limiting (multi-level) | pending | User/tenant + LLM API + vector DB + system tiers; stdlib `golang.org/x/time/rate` sufficient for single-node; needed before public exposure |
| 18 | Bulk SHA check at ingest | ✅ | `Store.GetAllFileSHAs()` added; single paginated scroll replaces O(N) RPCs |
| 19 | Concurrent dense+sparse embed | ✅ | Dense + sparse embed run concurrently per chunk via `sync.WaitGroup`; per-file ingestion still sequential |
| 20 | Production infra hardening | ✅ | Env-var overrides for all service addrs (`QDRANT_ADDR`, `OLLAMA_ADDR`, `SPLADE_ADDR`, `RERANKER_ADDR`, `LOGGER_LEVEL`); pinned Qdrant image; `restart: unless-stopped` all services; `GET /healthz` endpoint; compose healthchecks with proper `depends_on` conditions |
| 21 | Qdrant resource limits | ✅ | `deploy.resources.limits.memory: 2g` in compose; prevents OOM kill under 10k-doc load |
| 22 | Grafana dashboards | ✅ | Auto-provisioned via compose; panels: search latency p50/p90/p99, search rate, cache hit rate, embed latency, rerank latency + score delta, ingest throughput; datasource wired to Prometheus |
| 23 | Qdrant volume backup | ✅ | `scripts/backup-qdrant.sh` — docker-volume tar.gz snapshot; configurable retention (`KEEP_DAYS`); cron-friendly |
| 24 | LLM answer generation | ✅ | Ollama `/api/chat` streaming; "Lost in the Middle" chunk ordering (Liu et al. 2023); token budget enforcement; grounded prompt with inline citations; enable via `generator.enabled: true` + `generate: true` in search request |

**Enable SPLADE:** set `sparse_scorer.provider: splade` in `config/config.yaml`, then run `python cmd/splade/main.py`.

**Enable HyDE:** set `hyde.enabled: true` and `hyde.model: <ollama-llm>` in `config/config.yaml`. Ollama LLM must be pulled (`ollama pull <model>`). `num_docs: 1` (default) adds ~1-2s latency per query; `num_docs: 8` matches paper accuracy at ~8× latency (parallelized to ~1-2s via goroutines). Falls back to standard hybrid search on generation failure.

**Enable re-ranking:** set `reranker.enabled: true` in `config/config.yaml`, then run `python cmd/reranker/main.py` (`pip install sentence-transformers fastapi uvicorn`). Fetches `topK * candidate_mul` candidates from hybrid search, scores all with `cross-encoder/ms-marco-MiniLM-L-6-v2`, returns top-k. Adds ~100-400ms on CPU.

Clarrify:
1. Time-to-First-Token (TTFT) p90
2. Agent memory architecture: short-term memory for current session state, long-term memory storing user profiles and preferences through vector embeddings, and episodic memory that helps agents learn from past interactions. Episodic memory is what allows an agent to recall that a particular approach worked well for a similar question yesterday and apply that pattern again. The technical requirements follow from these needs: vector embedding storage for semantic memory, fast retrieval with sub-second latency, and reliable state persistence. Redis works as the memory substrate with vector search for semantic retrieval, key-value operations for state management, and pub/sub for agent communication. This unified data model doesn't require separate infrastructure for each memory type.

---

#### Technical Debt & Optimization Backlog

##### Maintainability & Structure
* **`IngestHandler.ServeHTTP` contains full ingest loop**: fetch + SHA-check + pipeline logic belongs in a service method, not the HTTP handler.
* **`runEval` / `evalMetrics` in `_test.go`**: eval is first-class concern. Move to `cmd/eval` binary, callable without `go test`.