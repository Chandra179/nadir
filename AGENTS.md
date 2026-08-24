# AGENTS.md

## Commands

> `Makefile` currently only defines a `run` target (`./scripts/local.sh`) — the `dev`/`test`/`ingest`/`search`/`generate`/`reset`/`vendor`/`docling*`/`reranker*`/`check` targets referenced below (and in older docs) don't exist as `make` targets; use the raw commands.

```bash
# Build
go build ./cmd/server

# Vendor deps (NOT committed — gitignored; run after adding imports)
go mod tidy && go mod vendor

# Dev: Qdrant + sidecars + server + auto-ingest
./scripts/local.sh               # addrs come from config/config.yaml (localhost)

# Run standalone (config/config.yaml, .env sourced)
go run ./cmd/server

# Tests
go test -short -count=1 ./...              # unit tests only, no Docker
go test -count=1 ./...                     # all tests, requires Qdrant
go test -run TestMatchPattern ./internal/ingest/   # focused pkg test

# Retrieval eval (needs live Qdrant + Ollama [+ reranker sidecar])
go run ./cmd/evalbench --ensure-ingest             # baseline/A-B reports → tests/eval/reports/
go run ./cmd/evalbench --no-rerank                 # measure reranker contribution
go run ./cmd/evalbench --golden tests/eval/golden.json --top-k 5 --runs 3

# Quick ops (server must be on :8100)
curl -X POST localhost:8100/ingest
curl -X POST localhost:8100/search -H "Content-Type: application/json" -d '{"query":"secant formula","top_k":10}'
curl -X POST localhost:8100/search -H "Content-Type: application/json" -d '{"query":"secant formula","top_k":5,"generate":true}' --no-buffer
curl -X DELETE localhost:6333/collections/documents_chunks   # reset Qdrant collection (REST :6333)
```

## Architecture

Single Go binary at `cmd/server/main.go`. Wiring in `internal/server/server.go` (`server.Server(ctx, cfg)`); HTTP handlers and route registration live in `internal/api/`.

```
POST /ingest → IngestHandler → ingest.Service (walk + SHA dedup) → Pipeline (chunk→embed→upsert)
POST /search → SearchHandler → search.Service (embed→hybrid search→[reranker]) → [generator]
GET  /healthz → 200
```

**Domain packages (under `internal/`):**
- `chunker/` — `Chunker` interface, `Chunk` value type, recursive + sentence-window providers, `ContextualText`
- `embedder/` — `Embedder`, `BatchEmbedder` interfaces, Ollama HTTP client
- `store/` — `Store` interface, `ScoredChunk` (flat value type), `SearchFilter`, Qdrant hybrid store (dense + BM25 sparse + RRF)
- `ingest/` — upload-file ingest (`.md`, SHA dedup, concurrent workers), chunk→enrich→embed→upsert; optional `Enricher` (HyPE questions/contextual intros)
- `search/` — multi-fragment hybrid search → rerank → semantic cache
- `chat/` — chat use-case (`Ask`: session mint → retrieve → buffered generate → detached persist); handlers only map request/result
- `generator/` — `Generator` interface, Ollama chat client, `buildPrompt`, `lostInMiddleOrder`
- `reranker/` — `Reranker` interface, cross-encoder sidecar client
- `cache/` — `SemanticCache` backed by a dedicated Qdrant collection
- `enrichment/` — index-time LLM enrichment over Ollama: HyPE hypothetical questions, contextual chunk intros (feature-flagged)
- `eval/` — golden-set retrieval metrics (HitRate/Recall/MRR/nDCG) used by `cmd/evalbench`; reports in `tests/eval/reports/`

**`internal/api/`** — HTTP handlers (`Search`, `Ingest`, `DeleteAllData`, `IngestStatus`, `IngestHistory`, `Stats`, `Retrieval`, `RetrievalSearch`, `Settings`, `HistorySessions`/`HistorySession`) and `NewRouter`. UI templates live as files in `dashboard/` (`embed.go` exposes them via go:embed); they are parsed once at startup (`render.go`) and rendered through the shared `renderHTML` helper — no markup in Go source.

**`internal/server/`** — `Server(ctx, cfg)`: builds dependencies, wires middleware, starts the gin engine.

**`internal/middleware/`** — gin middleware, registered outermost-first in `internal/server/server.go`: `Recovery→RequestID→Timeout→RequestLog→Metrics`.

**`services/`** — Python sidecars (each has own Dockerfile): `reranker/` (:5002), `docling/` (PDF→MD).

## Key rules

- Domain packages must NOT import `internal/api/`, `internal/server/`, or `internal/middleware/`
- Retry logic lives in `Pipeline` (ingest), never in `Embedder`/`Store`
- Chunk IDs = UUIDv5 over `filePath:lineStart:chunkIndex` (HyPE siblings append `:hype:<n>`) — deterministic upserts, no duplicates
- Config: `config/config.yaml` → `config/config.go applyEnv()` overrides. Known env vars: `QDRANT_ADDR`, `QDRANT_COLLECTION`, `OLLAMA_ADDR`, `EMBEDDER_API_KEY`, `RERANKER_ADDR`, `RERANKER_ENABLED`, `RERANKER_MODEL`, `LOGGER_LEVEL`, `SEMANTIC_CACHE_THRESHOLD`, `HYPE_ENABLED`, `CONTEXTUAL_ENABLED`
- Source dirs set via `source.paths` in config (list of paths); no env override for source dirs
- Embedder task prefixes (`embedder.query_prefix`/`document_prefix`) apply at call sites, not in the embedder; changing either requires a reindex
- Enrichment flags (`enrichment.hype.enabled`, `enrichment.contextual.enabled`) affect ingest only; enabling after a prior ingest requires a reindex

## Addresses: local vs Docker

`./scripts/local.sh` runs the host-side server against `config/config.yaml`'s localhost addresses directly, no env overrides needed. Inside Docker, `docker-compose.yml` overrides via env vars: Qdrant `qdrant:6334` (gRPC), reranker `reranker:5002`, Ollama `host.docker.internal:11434`.

## Features gated by config

| Feature | Config key | Requires |
|---------|-----------|----------|
| Answer generation | `generator.enabled` (on by default) | Ollama LLM; `POST /search` with `{"generate": true}` |
| Semantic cache | `semantic_cache.enabled` (on by default) | None (reuses Qdrant) |
| Reranker | `reranker.enabled` (on by default) | Reranker sidecar |
| HyPE | `enrichment.hype.enabled` (off by default) | Ollama LLM; reindex after enabling |
| Contextual retrieval | `enrichment.contextual.enabled` (off by default) | Ollama LLM; reindex after enabling |

`ollama_addr` defaults to `embedder.ollama_addr` when empty for generator (and enrichment falls back generator → embedder). The reranker cross-encoder is swappable via `reranker.model` (env `RERANKER_MODEL`; sidecar reloads it on restart).

## Sample data

`./scripts/local.sh` ingests from `source.paths` in config. A sample set lives at `samples/` (4 math markdown files). Add your own dirs to `source.paths` in `config/config.yaml`.
