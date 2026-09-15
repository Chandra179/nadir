# AGENTS.md

## Commands

The Makefile provides `run`, `test`, `race`, `vet`, `build`, and `check`
targets. It scopes Go checks to `./internal/platform/configuration`, `./cmd/...`, and `./internal/...`
so a local Python `venv/` is not discovered as a Go package.

```bash
# Build
go build ./cmd/api

# Vendor deps (NOT committed — gitignored; run after adding imports)
go mod tidy && go mod vendor

# Dev: Qdrant (Docker) + reranker (repo venv, explicit local device) + server + auto-ingest
./scripts/local.sh               # addrs come from config/config.yaml (localhost)

# Run standalone (config/config.yaml, .env sourced)
go run ./cmd/api

# Tests
go test -short -count=1 ./internal/platform/configuration ./cmd/... ./internal/... # unit tests only
go test -count=1 ./internal/platform/configuration ./cmd/... ./internal/...       # all Go tests
go test -run TestMatchPattern ./internal/knowledge/indexing/   # focused pkg test
make load-benchmark ARGS="--mode all --requests 30 --concurrency 8" # live p50/p95/p99 load evidence

# Quick ops (server must be on :8100)
curl -X POST localhost:8100/api/v1/documents
curl -X POST localhost:8100/api/v1/turns -H 'content-type: application/json' -d '{"query":"secant formula","generate":false}'
curl -X POST localhost:8100/api/v1/documents/reset                    # safely reset the Document collection

# Retrieval quality evaluation (Qdrant + embedder, optional reranker must be running)
go run ./cmd/evaluator --runs 3
go run ./cmd/evaluator --no-rerank --runs 3
```

## Architecture

The API binary is `cmd/api/main.go`; the evaluator is `cmd/evaluator/main.go`. Shared retrieval/indexing composition lives in `internal/platform/runtime/`; HTTP process lifecycle and feature wiring live in `internal/platform/server/server.go`; HTTP handlers and route registration live in `internal/transport/http/`.

```
POST /api/v1/documents → IngestHandler → ingest.Service (walk + SHA dedup) → Pipeline (chunk→embed→upsert)
POST /api/v1/turns → StartTurnHandler → chat.StartTurn (session mint or in-place edit prune → rewrite follow-up → retrieve → start generation supervisor)
GET  /api/v1/turns/:id/events → SSE adapter over the turn event log (replay via Last-Event-ID). Generation is owned by the chat service (subscribers never kill it); the turn is persisted by the supervisor at its terminal state
POST /api/v1/turns/:id/cancel → abort generation; the partial answer is kept and persisted
GET  /api/v1/health → 200
```

**Bounded contexts (under `internal/`):**
- `knowledge/` — Document intake and the Indexing pass: normalize, chunk,
  enrich, embed, deduplicate, and versioned publication.
- `retrieval/` — query rewriting, fragmentation, hybrid dense/BM25 search,
  RRF, cache lookup, reranking, and context selection.
- `conversation/` — Sessions, Chat turns, prompt construction, generation
  supervision, bounded event retention, cancellation, edit/prune, and history.
- `evaluation/` — development-only golden-set Retrieval evaluator and reports.

**Transport and infrastructure:**
- `transport/http/` — versioned JSON/SSE request mapping, status codes, route
  registration, and SSE adaptation only; shared response shapes live in
  `transport/http/contract/`.
- `adapters/qdrant/` — Qdrant clients, schema helpers, and document storage;
  domain lifecycle rules remain owned by their calling context.
- `adapters/ollama/` — embedding, enrichment, generation, and rewriting
  Adapters, each with explicit role-specific configuration.
- `adapters/reranker/` — cross-encoder sidecar Adapter.
- `platform/` — configuration, logging, observability, HTTP middleware, shared
  runtime composition, and process lifecycle.
- Optional profiling is disabled by default and is loopback-only when enabled;
  use a protected local access path such as an SSH tunnel for remote hosts.

The React/TypeScript/Tailwind client lives in `web/dashboard`; it is a separate
Node/Vite application run with `npm run dev`. Compose runs the backend and
sidecars only; a production static host may serve the built dashboard.
Python sidecars remain under `sidecars/` because they are separately deployed
processes.

**`sidecars/`** — Python sidecars (each has its own Dockerfile): `reranker/`
(:5002) and optional `document-converter/` (:5003, PDF→Markdown HTTP intake).

## Key rules

- Domain contexts must NOT import `internal/transport/http/`,
  `internal/platform/server/`, `internal/platform/httpmiddleware/`, or
  frontend code.
- Retry logic lives in `Pipeline` (ingest), never in `Embedder`/`Store`
- Chunk IDs = UUIDv5 over `filePath:sourceSHA:lineStart:chunkIndex` (HyPE siblings append `:hype:<n>`) — versioned deterministic replacement; old versions are deactivated and cleaned after the new version is active
- Config: `config/config.yaml` → `internal/platform/configuration/config.go` `applyEnv()` overrides. Known env vars include `QDRANT_ADDR`, `QDRANT_COLLECTION`, `OLLAMA_ADDR`, `EMBEDDER_MODEL`, `EMBEDDER_DIMENSIONS`, `EMBEDDER_QUERY_PREFIX`, `EMBEDDER_DOCUMENT_PREFIX`, `GENERATOR_ADDR`, `GENERATOR_MODEL`, `EMBEDDER_API_KEY`, `SOURCE_PATHS`, `SOURCE_MODE`, `SOURCE_IGNORE_PATTERNS`, `FUSION_ENABLED`, `FUSION_RRF_K`, `FUSION_DENSE_WEIGHT`, `FUSION_BM25_WEIGHT`, `FUSION_EXACT_MATCH_BOOST`, `FUSION_HEADER_MATCH_BOOST`, `FUSION_MIN_EXACT_TOKENS`, `FUSION_MIN_HEADER_TOKENS`, `RERANKER_ADDR`, `RERANKER_ENABLED`, `RERANKER_MODEL`, `RERANKER_ADAPTIVE_ENABLED`, `RERANKER_ADAPTIVE_MARGIN_THRESHOLD`, `RERANKER_DEVICE`, `RERANKER_BACKEND`, `RERANKER_TRUST_REMOTE_CODE`, `RERANKER_PORT`, `RERANKER_MAX_CONCURRENT`, `RERANKER_QUEUE_TIMEOUT`, `INFERENCE_PROFILE`, `INFERENCE_OLLAMA_MAX_CONCURRENT`, `INFERENCE_OLLAMA_QUEUE_TIMEOUT`, `INFERENCE_OLLAMA_KEEP_ALIVE`, `ADMISSION_<RETRIEVAL|RERANKING|GENERATION|EMBEDDING|INDEXING|DESTRUCTIVE>_MAX_CONCURRENT`, `ADMISSION_<RETRIEVAL|RERANKING|GENERATION|EMBEDDING|INDEXING|DESTRUCTIVE>_QUEUE_TIMEOUT`, `HISTORY_SESSION_PAGE_SIZE`, `HISTORY_TURN_PAGE_SIZE`, `PROFILING_ENABLED`, `PROFILING_ADDR`, `LOGGER_LEVEL`, `SEMANTIC_CACHE_THRESHOLD`, `HYPE_ENABLED`, `HYPE_ADDR`, `HYPE_MODEL`, `CONTEXTUAL_ENABLED`, `CONTEXTUAL_ADDR`, `CONTEXTUAL_MODEL`, `REWRITE_ENABLED`, `REWRITE_ADDR`, `REWRITE_MODEL`, `REWRITE_TURNS`, `HISTORY_ENABLED`, `HISTORY_COLLECTION`, `DOCLING_ENABLED`, `DOCLING_ADDR`
- Source dirs are configured by `source.paths`; `SOURCE_PATHS` is a comma-separated override used by Compose and container deployments
- External Ollama/sidecar request timeouts are configured per role in `config/config.yaml`; constructors retain defaults only for direct package tests. Enabled LLM roles must declare their own `ollama_addr` and `model`; they do not inherit another role's endpoint.
- Embedder task prefixes (`embedder.query_prefix`/`document_prefix`) apply at call sites, not in the embedder; changing either requires a reindex
- Enrichment flags (`enrichment.hype.enabled`, `enrichment.contextual.enabled`) affect ingest only; enabling after a prior ingest requires a reindex
- Retrieval fusion is opt-in under `search.fusion`; it uses weighted rank-RRF plus optional exact/header boosts and must be compared against the default Qdrant RRF path on the golden set before enabling.

## Addresses: local vs Docker

`./scripts/local.sh` runs the host-side server against `config/config.yaml`'s localhost addresses directly and prints the local dashboard command. The base Compose stack runs the backend services and is CPU-safe for Linux, Windows Docker Desktop, and macOS; layer `deploy/compose/docker-compose.gpu.yml` only on Linux or Windows WSL2 with NVIDIA support. The dashboard is not a Compose service.

The API exposes bounded process-local operation metrics at `/debug/metrics`.
Request/trace IDs and domain child operation IDs are included in structured
logs; this endpoint is diagnostic telemetry, not a distributed metrics store.

## Features gated by config

| Feature | Config key | Requires |
|---------|-----------|----------|
| Answer generation | `generator.enabled` (on by default) | Ollama LLM; dashboard or `POST /api/v1/turns` with `generate=true` |
| Semantic cache | `semantic_cache.enabled` (on by default) | None (reuses Qdrant) |
| Reranker | `reranker.enabled` (on by default) | Reranker sidecar |
| Query rewriting | `rewriter.enabled` (on by default) | Ollama LLM; follow-up turns only (+1 LLM call); chat history enabled |
| HyPE | `enrichment.hype.enabled` (off by default) | Ollama LLM; reindex after enabling |
| Contextual retrieval | `enrichment.contextual.enabled` (off by default) | Ollama LLM; reindex after enabling |
| PDF document intake | `docling.enabled` (off by default) | Docling sidecar; source PDFs are converted before indexing |

Every enabled LLM role must declare its own `ollama_addr` and `model`; generator, rewriter, HyPE, and contextual enrichment do not inherit another role's endpoint or model. The default `inference.profile: local` shares one Ollama Gate across embedding, rewriting, enrichment, and streaming generation, uses a finite `keep_alive`, and limits the reranker to one explicit CPU operation. Set `RERANKER_DEVICE=cuda` and `RERANKER_BACKEND=torch` only with the GPU Compose override and a measured hardware budget; `auto` is reserved for `inference.profile: custom`. The base Compose stack is CPU-safe (`RERANKER_GPU=0`, `RERANKER_DEVICE=cpu`); `deploy/compose/docker-compose.gpu.yml` adds the CUDA build and NVIDIA reservation for Linux/Windows WSL2. The dev flow (`local.sh`) runs the sidecar from the repo `venv/` on the host with the explicit local CPU profile and only starts Qdrant via Docker. Apple Silicon should use the CPU `torch` backend; the AVX2 quantized bake is skipped for portable builds.
The `admission` section adds process-wide finite queues for expensive and destructive operations. These budgets coordinate one API process only; they do not make Chat, Indexing, or model serving distributed-safe.

## Sample data

`./scripts/local.sh` ingests from `source.paths` in config. A sample set lives at `samples/` (4 math markdown files). Add your own dirs to `source.paths` in `config/config.yaml`.
