---
title: "RAG"
aliases: []
tags: [rag]
created: "2026-06-13"
---

# RAG

Documents flow through a RAG pipeline in eight stages. Six are implemented (ingest, chunk, embed, retrieve, generate, evaluate); test coverage is minimal (unit tests only); monitor is scaffolded but broken on `main`. This note documents the `nadir` implementation as of `main` — config defaults, exact mechanics, and where the code has gaps.

> **Before reading:** `nadir` is a separate Go repo (github.com/Chandra179/nadir); this vault note documents it. Retrieval-quality concepts (recall@k, MRR, NDCG, faithfulness, LLM-as-judge) live in `ai/evaluation.md`.

## Ingestion

`IngestService.Run` lists files across one or more roots (`source.path` plus `source.paths`, merged and deduped via `AllPaths()`), filters out paths matching `ingest.ignore_patterns` (glob, with `dir/**` prefix matching), diffs each file's SHA-256 against the store in a single paginated scroll (`GetAllFileSHAs`, page size 1000), and dispatches changed files to 8 concurrent workers (`const ingestWorkers = 8`). Unchanged files are skipped; changed files are upserted in place by deterministic point ID.

* **Deterministic IDs** — `chunkID(filePath, lineStart, chunkIndex)` = UUIDv5 (`uuid.NewSHA1` over a private namespace). Same input always maps to the same point, so upserts replace rather than duplicate.
* **Contextual embedding** — before embedding, each chunk is prefixed with `filePath > header\n` (or just `filePath\n` when the chunk has no heading) (`chunker.ContextualText`). This anchors chunk semantics to document structure without altering stored text. Embedding is batched: `OllamaEmbedder.EmbedBatch` sends every chunk in a file to Ollama `/api/embed` in one round-trip (`Pipeline` uses the `BatchEmbedder` fast-path).
* **Qdrant collections** — auto-configured on startup: dense vectors with Cosine distance, a named sparse vector with IDF modifier, a full-text index on `text`, and a keyword index on `file_path`.

***

## Chunking

Two chunkers, selected by `chunker.provider`:

1. **Recursive** (`recursive`, default) — extracts sections by heading, then splits oversized sections by paragraph, then by sentence, then by word. Separators in order: `\n\n`, `\n`, `. `, ` `. A TOC heuristic drops chunks whose lines are mostly bare page numbers (threshold 0.6).
2. **Sentence Window** (`sentence-window`) — indexes at sentence granularity but stores a surrounding window (default 3 sentences before and after) as retrieval context.

**Chunk size is measured in UTF-8 runes, not tokens.** Default `chunk_size: 512`, `chunk_overlap: 64` (both any positive integer, unconstrained). The 4-chars/token estimate is applied only later, at generator prompt truncation.

***

## Embedding & Storage

`OllamaEmbedder` calls Ollama `/api/embed` with `nomic-embed-text` (768 dimensions).

```yaml
embedder:
  provider: "ollama"
  model: "nomic-embed-text"
  ollama_addr: "http://localhost:11434"
  dimensions: 768
```

Qdrant stores dense vectors alongside payload metadata:

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

**Distance metric:** Cosine, exclusively (dense collection and semantic cache).

**Server-side vs client-side hybrid.** The store supports both, selected at query time:
* **Client-side (active)** — dense `Search` + BM25 `Scroll` + client SPLADE rescore + manual RRF. This is the only path wired in `server.go` today.
* **Server-side (exists, not wired)** — `QueryPoints` with dense+sparse prefetch legs and Qdrant-native `Fusion_RRF` in a single round-trip. Gated on `store.WithSparseEmbedder(...)`, which `server.go` never calls, so sparse vectors are never stored at ingest and this branch is unreachable in the current build.

***

## Retrieval

```
Query
  │
  ▼
Semantic Cache ──── hit (score ≥ threshold, generate=false) ──► return cached result
  │ miss
  ▼
Hybrid Search  (client-side, active)
  ├── Dense: Qdrant ANN (nomic-embed-text vec)
  └── Sparse: BM25 Scroll → SPLADE rescore
         └── RRF fusion (k=60): score = 1/(60+denseRnk) + 1/(60+bm25Rnk)
  │
  ▼
Reranker
  └── cross-encoder/ms-marco-MiniLM-L-6-v2 (sidecar :5002)
  │
  ▼
Results: []{ file_path, header, line_start, score, text }
```

### Semantic Cache

A dedicated Qdrant collection (`search_cache`) caches results keyed by query-embedding similarity. On a hit (top-1 cosine ≥ threshold) the cached result returns immediately, skipping the retrieval pipeline — store search, reranker, chunk filter, and generator (the cache still embeds the query to perform the lookup). The cache hit path runs **only when `generate=false`** and `query` is non-empty — generation and keyword-only requests always run the full pipeline. On a miss, the pipeline runs and the result is written to cache asynchronously (`go semanticCache.Set(...)`); generate requests also populate the cache even though they never read from it.

```json
{
  "cached_at": "2026-04-26T15:23:38Z",
  "results_json": "{Variants\",\"LineStart\":327,\"ChunkIndex\":0,\"Vector\":null,\"SparseIndices\":null}",
  "query": "In Monte Carlo Tree Search, how do we calculate UCB?"
}
```

* **TTL** — default 24h; `0` disables expiry.
* **Threshold** (cosine):
  * `0.85–0.90`: high recall, allows paraphrased queries
  * `0.90–0.95`: balanced (default `0.90`)
  * `>0.95`: near-identical only

### Hybrid Search

Hybrid search fuses a dense leg and a sparse (BM25) leg. The client-side path fetches `topK × prefetch_mul` (default ×5) candidates per leg, then fuses via RRF (k=60).

> **Multi-fragment queries.** `SearchService.multiSearch` splits the query on `[.?;]+\s*`, runs `HybridSearch` per fragment, dedups by chunk key (keeping the best score), and re-sorts. The `topK` passed into the store is already `topK × candidate_mul` when a reranker is wired, so total candidates per leg scale as `topK × candidate_mul × prefetch_mul`.

### Payload Filtering

`HybridSearch` and `KeywordSearch` accept a `*SearchFilter` whose non-empty fields are ANDed:
* `file_path` — restrict to a specific file
* `header` — restrict to a specific section
* `source_sha` — restrict to a specific document version

Standalone dense `Search(ctx, vector, topK)` takes **no filter** — only the hybrid and keyword paths pre-filter. (The dense leg *inside* hybrid does apply the filter.)

### Reranking

A cross-encoder re-ranks candidates from vector search using the chunk's Window Text.
* **Oversampling** — the retrieval stage fetches `topK × candidate_mul` candidates (default `candidate_mul: 2`; code fallback 3) so the reranker has high-quality options. The store-level hybrid prefetch is separate: ×5 per leg.
* **Contextual scoring** — the Window Text (chunk plus surrounding context) is passed to the cross-encoder.
* **Final sorting** — candidates are re-scored by deep semantic relevance and sorted, promoting the best matches to the top for the LLM.

***

***

## Generator

`OllamaGenerator` streams an answer grounded in retrieved chunks via Ollama `/api/chat`.

**Prompt construction:**
* **Lost in the Middle** ordering (Liu et al. 2023) — highest-scored chunk at position `[1]`, lowest in the middle, second-highest at the end. Reduces LLM degradation on long context.
* **Token budget** — chunks truncated at roughly 1 token ≈ 4 chars; default `max_context_tokens: 2800` (~70% of a 4k context window).
* **Citation** — the prompt instructs the model to cite inline as `[1]`, `[2]`, etc. This instruction lives in a single `user`-role message; no separate `system` message is sent.

**Usage:** `POST /search` with `"generate": true`. Response is `text/plain` with chunked transfer encoding (streaming).

```yaml
generator:
  enabled: true
  model: "gemma3:1b"
  max_context_tokens: 2800
```

***

## Evaluate

> **Status: implemented (`internal/eval/` + `cmd/eval/`).**

Two eval modes, both driven by a golden set YAML and run through the `cmd/eval` CLI:

**Retrieval eval** (`-mode retrieval`) — `eval.Runner` runs each golden query through a `SearchService` (rebuilt with the same reranker/chunk-filter wiring as the server, minus semantic cache and generator) and `eval.Aggregate` scores the ranked list:

| Metric | Notes |
|---|---|
| Recall@5, Recall@10 | unique-relevant / total-relevant (dedup at chunk granularity) |
| Precision@5 | hits / k (denominator is k) |
| MRR (ReciprocalRank) | 1/rank of first relevant |
| Success@5 | 1 if any relevant in top-5 |
| MAP (AveragePrecision) | area under P-R curve |
| NDCG@10 | linear-gain (Järvelin & Kekäläinen 2002) |
| NDCG@10 (exp) | exponential-gain `(2^rel − 1)/log2(i+1)` — BEIR-style |

Graded relevance: `relevance: {file: grade}` with 0=irrelevant, 1=marginal, 2=relevant, 3=highly; `expected_files` is the binary special case (grade=1). Path matching is suffix-based (`MatchFile`), so `math/trigonometry.md` matches `gitbook/math/trigonometry.md`. **Bootstrap 95% CIs** are printed for Recall@5, Recall@10, NDCG@10, and MAP (1000 resamples, fixed seed). `-granularity chunk` scores at passage level (paper-comparable); default is `file` (deduped).

**RAG eval** (`-mode rag`) — `eval.RAGASEvaluator` runs the full RAG loop per query (retrieve → `GeneratorAdapter` generates → `OllamaJudge` scores) and reports four RAGAS metrics (Es et al. 2023, arxiv 2309.15217):

| Metric | Method |
|---|---|
| Faithfulness | decompose answer → statements; verify each against context; ratio supported |
| Answer Relevance | LLM rates answer-vs-query 0–1 |
| Context Precision | LLM rates each chunk 0–1, weighted by `1/log2(k+1)` |
| Context Recall | decompose `expected_answer` → statements; check attributability to context (requires `expected_answer`; otherwise `N/A`) |

The judge calls Ollama's OpenAI-compatible `/v1/chat/completions`; the judge model defaults to `generator.model` (override with `-judge-model`). `-mode both` runs retrieval then RAGAS in one pass.

**CLI:** `go run ./cmd/eval -golden my-golden.yaml -fetch-k 10 -mode retrieval` (Make targets: `eval`, `eval-rag`, `eval-both`, `eval-chunk`, each requiring `golden=my-golden.yaml`). Flags: `-config` (default `config/config.yaml`), `-golden` (required), `-fetch-k` (default 10), `-mode` (retrieval|rag|both), `-granularity` (file|chunk), `-judge-model`. A warning is printed when `n < 50` (BEIR min ~1k).

**Tests:** `internal/eval/{metrics,ragas,runner}_test.go` — metric math, RAGAS scoring with a stub judge, and runner aggregation. No integration tests against live Qdrant/Ollama.

> The `EVAL_LLM_BASE_URL` / `EVAL_LLM_MODEL` / `EVAL_HISTORY_PATH` entries were removed from `.env.example` — they were never read by any code.

For the conceptual basis — perplexity, BLEU/ROUGE/METEOR/BERTScore, LLM-as-judge, benchmarks — see `ai/evaluation.md`.

***

## Test

> **Status: minimal (unit tests only, no Docker/Qdrant required).**

* `make test` — `go test -short -count=1 ./...`
* `make test-all` — `go test -count=1 ./...`

Five test files cover five units:
* `internal/engine/file_lister_local_test.go` — glob ignore-pattern matching.
* `internal/eval/metrics_test.go` — retrieval metric math (Recall/Precision/MRR/MAP/NDCG/Bootstrap CI).
* `internal/eval/ragas_test.go` — RAGAS scoring with a stub `LLMJudge`.
* `internal/eval/runner_test.go` — runner aggregation + granularity.

No chunker tests, no integration tests against live Qdrant/Ollama, no k6 load scripts (the `k6` repo topic is aspirational).

***

## Monitor

> **Status: scaffolded, broken on `main`.**

* `docker-compose.yml` defines `prometheus` and `node-exporter` but **not** `grafana` or `k6`.
* `scripts/dev-local.sh` runs `docker compose up ... grafana` — referencing a service the compose file does not define, so `make dev` errors on that line.
* The compose file mounts `./config/prometheus.yml` and `./config/recording_rules.yml`, but neither file exists in `config/` (only `config.go` and `config.yaml`); Prometheus would fail to start as committed.
* The Go app exposes **no `/metrics` endpoint** — only `POST /search`, `POST /ingest`, `GET /healthz`. OpenTelemetry metric SDK packages are indirect dependencies (via the logger) and unused for app metrics.

***

## Known Gaps & Drift

1. **Server-side hybrid search is not wired.** `server.go` calls `store.WithSparseScorer(...)` (client-side SPLADE rescore) but never `store.WithSparseEmbedder(...)`. Sparse vectors are not stored at ingest, so `hybridSearchServer` (the `QueryPoints` + native RRF path) is unreachable in the current build. Only client-side hybrid is active. (`cmd/eval`'s `buildSearcher` mirrors this — same gap.)
2. **Modified files leave orphan chunks.** `IngestService.Run` upserts changed files by deterministic ID but never calls `DeleteByFile` (the method exists on the store, but is never invoked on the change path). Because `chunkID` is derived from `filePath:lineStart:chunkIndex`, an edit that shifts a chunk's `line_start` produces a new point while the old point remains — stale chunks are not garbage-collected.
3. **Monitor infra is half-built.** See §Monitor — missing Prometheus config files, undefined Grafana service, no app metrics endpoint.
4. **`embedder.api_key` / `EMBEDDER_API_KEY` is a dead config field.** It is parsed into `config.Embedder.APIKey` and surfaced in `.env.example`, but `OllamaEmbedder` is constructed with only `(addr, model, dimensions)` and sends no `Authorization` header, so the key is never used. (The RAGAS judge and chunk filter accept an API key, but both are constructed with `""`.)

***

## References

* arxiv 2410.19572 — LLM chunk filter (+10pp PopQA).
* Liu et al., 2023 — "Lost in the Middle: How Language Models Use Long Contexts."
* Anthropic, 2024 — contextual embedding (`filePath > header` prefix).
* Järvelin & Kekäläinen, 2002 — NDCG (ACM TOIS 20(4):422–446).
* Manning, Raghavan & Schütze, 2008 — *Introduction to Information Retrieval* (MAP).
* Es et al., 2023 — RAGAS (arxiv 2309.15217).
* Thakur et al., 2021 — BEIR (NeurIPS, arxiv 2104.08663).
* Formal et al., 2021 — SPLADE (arxiv 2107.05720).
