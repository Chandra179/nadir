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

# Dev: Qdrant (Docker) + reranker (repo venv, host GPU) + server + auto-ingest
./scripts/local.sh               # addrs come from config/config.yaml (localhost)

# Run standalone (config/config.yaml, .env sourced)
go run ./cmd/api

# Tests
go test -short -count=1 ./internal/platform/configuration ./cmd/... ./internal/... # unit tests only
go test -count=1 ./internal/platform/configuration ./cmd/... ./internal/...       # all Go tests
go test -run TestMatchPattern ./internal/knowledge/indexing/   # focused pkg test

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
- Config: `config/config.yaml` → `internal/platform/configuration/config.go` `applyEnv()` overrides. Known env vars include `QDRANT_ADDR`, `QDRANT_COLLECTION`, `OLLAMA_ADDR`, `GENERATOR_ADDR`, `GENERATOR_MODEL`, `EMBEDDER_API_KEY`, `SOURCE_PATHS`, `SOURCE_IGNORE_PATTERNS`, `RERANKER_ADDR`, `RERANKER_ENABLED`, `RERANKER_MODEL`, `LOGGER_LEVEL`, `SEMANTIC_CACHE_THRESHOLD`, `HYPE_ENABLED`, `HYPE_ADDR`, `HYPE_MODEL`, `CONTEXTUAL_ENABLED`, `CONTEXTUAL_ADDR`, `CONTEXTUAL_MODEL`, `REWRITE_ENABLED`, `REWRITE_ADDR`, `REWRITE_MODEL`, `REWRITE_TURNS`, `HISTORY_ENABLED`, `HISTORY_COLLECTION`, `DOCLING_ENABLED`, `DOCLING_ADDR`
- Source dirs are configured by `source.paths`; `SOURCE_PATHS` is a comma-separated override used by Compose and container deployments
- External Ollama/sidecar request timeouts are configured per role in `config/config.yaml`; constructors retain defaults only for direct package tests. Enabled LLM roles must declare their own `ollama_addr` and `model`; they do not inherit another role's endpoint.
- Embedder task prefixes (`embedder.query_prefix`/`document_prefix`) apply at call sites, not in the embedder; changing either requires a reindex
- Enrichment flags (`enrichment.hype.enabled`, `enrichment.contextual.enabled`) affect ingest only; enabling after a prior ingest requires a reindex

## Addresses: local vs Docker

`./scripts/local.sh` runs the host-side server against `config/config.yaml`'s localhost addresses directly and prints the local dashboard command. The base Compose stack runs the backend services and is CPU-safe for Linux, Windows Docker Desktop, and macOS; layer `deploy/compose/docker-compose.gpu.yml` only on Linux or Windows WSL2 with NVIDIA support. The dashboard is not a Compose service.

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

Every enabled LLM role must declare its own `ollama_addr` and `model`; generator, rewriter, HyPE, and contextual enrichment do not inherit another role's endpoint or model. The reranker cross-encoder is swappable via `reranker.model` (env `RERANKER_MODEL`; sidecar reloads it on restart) and supports `RERANKER_BACKEND` (`onnx`, `torch-int8`, or `torch`). The base Compose stack is CPU-safe (`RERANKER_GPU=0`, `RERANKER_DEVICE=cpu`); `deploy/compose/docker-compose.gpu.yml` adds the CUDA build and NVIDIA reservation for Linux/Windows WSL2. The dev flow (`local.sh`) runs the sidecar from the repo `venv/` on the host (`RERANKER_DEVICE=auto`, like Ollama) and only starts Qdrant via Docker. Apple Silicon should use the CPU `torch` backend; the AVX2 quantized bake is skipped for portable builds.

## Sample data

`./scripts/local.sh` ingests from `source.paths` in config. A sample set lives at `samples/` (4 math markdown files). Add your own dirs to `source.paths` in `config/config.yaml`.
