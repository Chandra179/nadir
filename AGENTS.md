# AGENTS.md

## Commands

The Makefile provides `run`, `test`, `race`, `vet`, `build`, and `check`
targets. It scopes Go checks to `./config`, `./cmd/...`, and `./internal/...`
so a local Python `venv/` is not discovered as a Go package.

```bash
# Build
go build ./cmd/server

# Vendor deps (NOT committed — gitignored; run after adding imports)
go mod tidy && go mod vendor

# Dev: Qdrant (Docker) + reranker (repo venv, host GPU) + server + auto-ingest
./scripts/local.sh               # addrs come from config/config.yaml (localhost)

# Run standalone (config/config.yaml, .env sourced)
go run ./cmd/server

# Tests
go test -short -count=1 ./config ./cmd/... ./internal/... # unit tests only
go test -count=1 ./config ./cmd/... ./internal/...       # all Go tests
go test -run TestMatchPattern ./internal/ingest/   # focused pkg test

# Quick ops (server must be on :8100)
curl -X POST localhost:8100/ingest
curl -X POST localhost:8100/retrieval/search --data-urlencode "query=secant formula"
curl -X DELETE localhost:6333/collections/documents_chunks   # reset Qdrant collection (REST :6333)
```

## Architecture

Single Go binary at `cmd/server/main.go`. Wiring in `internal/server/server.go` (`server.Server(ctx, cfg)`); HTTP handlers and route registration live in `internal/api/`.

```
POST /ingest → IngestHandler → ingest.Service (walk + SHA dedup) → Pipeline (chunk→embed→upsert)
POST /retrieval/search → RetrievalSearchHandler → chat.StartTurn (session mint or in-place edit prune → rewrite follow-up → retrieve → start generation supervisor)
GET  /retrieval/turns/:id/events → SSE adapter over the turn event log (replay via Last-Event-ID). Generation is owned by the chat service (subscribers never kill it); the turn is persisted by the supervisor at its terminal state
POST /retrieval/turns/:id/cancel → abort generation; the partial answer is kept and persisted
GET  /healthz → 200
```

**Domain packages (under `internal/`):**
- `chunker/` — `Chunker` interface, `Chunk` value type, recursive + sentence-window providers, `ContextualText`
- `embedder/` — `Embedder`, `BatchEmbedder` interfaces, Ollama HTTP client
- `store/` — document-corpus `Store` interface and Qdrant hybrid Adapter (dense + BM25 sparse + RRF); storage chunk/filter types stay behind `search`'s caller-facing seam
- `ingest/` — document intake (`.md`, optional `.pdf` through Docling), SHA dedup, bounded indexing pass, chunk→enrich→embed→replace; consumes `enrichment.Enricher`
- `search/` — Retrieval-owned request/result types; multi-fragment hybrid search → rerank → semantic cache
- `chat/` — chat use-case (`StartTurn`: session mint or in-place edit prune → rewrite follow-up → retrieve → start generation supervisor; owns the turn event log, persistence at terminal state, and `CancelTurn`); handlers only map request/result
- `generator/` — `Generator` interface (`Generate(ctx, prompt) <-chan Event` with typed `TokenEvent`/`ErrorEvent`/`DoneEvent`), Ollama streaming client; prompt building lives in `internal/chat/prompt.go`
- `rewriter/` — `Rewriter` interface, Ollama client rewriting conversational follow-ups into standalone search queries (feature-flagged)
- `reranker/` — `Reranker` interface, cross-encoder sidecar client
- `cache/` — `SemanticCache` backed by a dedicated Qdrant collection
- `enrichment/` — `Enricher` interface + index-time LLM enrichment over Ollama: HyPE hypothetical questions, contextual chunk intros (feature-flagged)

**`internal/api/`** — HTTP transport, grouped by feature. Root package: `NewRouter` (route consts + registration), `NewDependencies` (DI; resolves the default top_k once), the page shell (`Retrieval`, `HistorySession`), and the `Ingest`/`DeleteAllData` handlers. Sub-packages: `chat/` (turn lifecycle — start, SSE event stream, cancel — plus the turn views), `history/` (sidebar session list, single-session delete, and guarded delete-all), `internal/render/` (template engine). UI templates live as files in `dashboard/` (`embed.go` exposes them via go:embed); they are parsed once at startup and rendered through the shared render engine — no markup in Go source.

**`internal/server/`** — `Server(ctx, cfg)`: builds dependencies, wires middleware, starts the gin engine.

**`internal/middleware/`** — gin middleware, registered outermost-first in `internal/server/server.go`: `Recovery→RequestID→Timeout→RequestLog`.

**`services/`** — Python sidecars (each has own Dockerfile): `reranker/` (:5002), optional `docling/` (:5003, PDF→Markdown HTTP intake).

**`internal/qdrantutil/`** — shared Qdrant client set, dense collection setup,
payload primitive codecs, and point-ID decoding. It is infrastructure shared
by the document store, history, and semantic cache; their domain lifecycles
remain separate.

## Key rules

- Domain packages must NOT import `internal/api/`, `internal/server/`, or `internal/middleware/`
- Retry logic lives in `Pipeline` (ingest), never in `Embedder`/`Store`
- Chunk IDs = UUIDv5 over `filePath:lineStart:chunkIndex` (HyPE siblings append `:hype:<n>`) — deterministic upserts, no duplicates
- Config: `config/config.yaml` → `config/config.go applyEnv()` overrides. Known env vars include `QDRANT_ADDR`, `QDRANT_COLLECTION`, `OLLAMA_ADDR`, `GENERATOR_ADDR`, `GENERATOR_MODEL`, `EMBEDDER_API_KEY`, `SOURCE_PATHS`, `SOURCE_IGNORE_PATTERNS`, `RERANKER_ADDR`, `RERANKER_ENABLED`, `RERANKER_MODEL`, `LOGGER_LEVEL`, `SEMANTIC_CACHE_THRESHOLD`, `HYPE_ENABLED`, `HYPE_ADDR`, `HYPE_MODEL`, `CONTEXTUAL_ENABLED`, `CONTEXTUAL_ADDR`, `CONTEXTUAL_MODEL`, `REWRITE_ENABLED`, `REWRITE_ADDR`, `REWRITE_MODEL`, `REWRITE_TURNS`, `HISTORY_ENABLED`, `HISTORY_COLLECTION`, `DOCLING_ENABLED`, `DOCLING_ADDR`
- Source dirs are configured by `source.paths`; `SOURCE_PATHS` is a comma-separated override used by Compose and container deployments
- External Ollama/sidecar request timeouts are configured per role in `config/config.yaml`; constructors retain defaults only for direct package tests. Enabled LLM roles must declare their own `ollama_addr` and `model`; they do not inherit another role's endpoint.
- Embedder task prefixes (`embedder.query_prefix`/`document_prefix`) apply at call sites, not in the embedder; changing either requires a reindex
- Enrichment flags (`enrichment.hype.enabled`, `enrichment.contextual.enabled`) affect ingest only; enabling after a prior ingest requires a reindex

## Addresses: local vs Docker

`./scripts/local.sh` runs the host-side server against `config/config.yaml`'s localhost addresses directly. The base Compose stack is CPU-safe for Linux, Windows Docker Desktop, and macOS; layer `docker-compose.gpu.yml` only on Linux or Windows WSL2 with NVIDIA support.

## Features gated by config

| Feature | Config key | Requires |
|---------|-----------|----------|
| Answer generation | `generator.enabled` (on by default) | Ollama LLM; chat UI or `POST /retrieval/search` with `generate=true` |
| Semantic cache | `semantic_cache.enabled` (on by default) | None (reuses Qdrant) |
| Reranker | `reranker.enabled` (on by default) | Reranker sidecar |
| Query rewriting | `rewriter.enabled` (on by default) | Ollama LLM; follow-up turns only (+1 LLM call); chat history enabled |
| HyPE | `enrichment.hype.enabled` (off by default) | Ollama LLM; reindex after enabling |
| Contextual retrieval | `enrichment.contextual.enabled` (off by default) | Ollama LLM; reindex after enabling |
| PDF document intake | `docling.enabled` (off by default) | Docling sidecar; source PDFs are converted before indexing |

Every enabled LLM role must declare its own `ollama_addr` and `model`; generator, rewriter, HyPE, and contextual enrichment do not inherit another role's endpoint or model. The reranker cross-encoder is swappable via `reranker.model` (env `RERANKER_MODEL`; sidecar reloads it on restart) and supports `RERANKER_BACKEND` (`onnx`, `torch-int8`, or `torch`). The base Compose stack is CPU-safe (`RERANKER_GPU=0`, `RERANKER_DEVICE=cpu`); `docker-compose.gpu.yml` adds the CUDA build and NVIDIA reservation for Linux/Windows WSL2. The dev flow (`local.sh`) runs the sidecar from the repo `venv/` on the host (`RERANKER_DEVICE=auto`, like Ollama) and only starts Qdrant via Docker. Apple Silicon should use the CPU `torch` backend; the AVX2 quantized bake is skipped for portable builds.

## Sample data

`./scripts/local.sh` ingests from `source.paths` in config. A sample set lives at `samples/` (4 math markdown files). Add your own dirs to `source.paths` in `config/config.yaml`.
