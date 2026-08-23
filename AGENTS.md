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
- `chunker/` — `Chunker` interface, `Chunk` value type, `RecursiveChunker`, `SentenceWindowChunker`, `ContextualText`
- `embedder/` — `Embedder`, `BatchEmbedder` interfaces, `OllamaEmbedder`
- `store/` — `Store` interface, `ScoredChunk` (flat value type), `SearchFilter`, `QdrantStore`
- `ingest/` — `Processor` interface, `Pipeline` (chunk→embed→upsert), `Service` (dir walk + SHA dedup + concurrent processing)
- `search/` — `Service` (multi-fragment hybrid search → rerank)
- `generator/` — `Generator` interface, `OllamaGenerator`, `buildPrompt`, `lostInMiddleOrder`
- `reranker/` — `Reranker` interface, `HTTPReranker` (cross-encoder sidecar client)
- `cache/` — `SemanticCache` backed by a dedicated Qdrant collection

**`internal/api/`** — HTTP handlers (`Search`, `Ingest`, `DeleteAllData`, `IngestStatus`, `IngestHistory`, `Stats`, `Dashboard`, `Retrieval`, `RetrievalSearch`) and `NewRouter`, registering them all on the gin engine.

**`internal/server/`** — `Server(ctx, cfg)`: builds dependencies, wires middleware, starts the gin engine.

**`internal/middleware/`** — gin middleware, registered outermost-first in `internal/server/server.go`: `Recovery→RequestID→Timeout→RequestLog→Metrics`.

**`services/`** — Python sidecars (each has own Dockerfile): `reranker/` (:5002), `docling/` (PDF→MD).

## Key rules

- Domain packages must NOT import `internal/api/`, `internal/server/`, or `internal/middleware/`
- Retry logic lives in `Pipeline`, never in `Embedder`/`Store`
- Chunk IDs = UUIDv5 over `filePath:lineStart:chunkIndex` — deterministic upserts, no duplicates
- Config: `config/config.yaml` → `config/config.go applyEnv()` overrides. Known env vars: `QDRANT_ADDR`, `QDRANT_COLLECTION`, `OLLAMA_ADDR`, `EMBEDDER_API_KEY`, `RERANKER_ADDR`, `RERANKER_ENABLED`, `LOGGER_LEVEL`, `SEMANTIC_CACHE_THRESHOLD`
- Source dirs set via `source.paths` in config (list of paths); no env override for source dirs

## Addresses: local vs Docker

`./scripts/local.sh` runs the host-side server against `config/config.yaml`'s localhost addresses directly, no env overrides needed. Inside Docker, `docker-compose.yml` overrides via env vars: Qdrant `qdrant:6334` (gRPC), reranker `reranker:5002`, Ollama `host.docker.internal:11434`.

## Features gated by config

| Feature | Config key | Requires |
|---------|-----------|----------|
| Answer generation | `generator.enabled` (on by default) | Ollama LLM; `POST /search` with `{"generate": true}` |
| Semantic cache | `semantic_cache.enabled` (on by default) | None (reuses Qdrant) |
| Reranker | `reranker.enabled` (on by default) | Reranker sidecar |

`ollama_addr` defaults to `embedder.ollama_addr` when empty for generator.

## Sample data

`./scripts/local.sh` ingests from `source.paths` in config. A sample set lives at `samples/` (4 math markdown files). Add your own dirs to `source.paths` in `config/config.yaml`.
