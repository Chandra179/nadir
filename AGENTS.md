# AGENTS.md

## Commands

```bash
# Build
go build ./cmd/server

# Vendor deps (NOT committed — gitignored; run after adding imports)
make vendor                     # go mod tidy && go mod vendor

# Dev: Qdrant + sidecars + Prometheus + server + auto-ingest
make dev                        # runs scripts/dev-local.sh (env overrides → localhost)

# Run standalone (config/config.yaml, .env sourced)
make run                        # go run ./cmd/server

# Tests
make test                       # unit tests only, no Docker (-short -count=1 ./...)
make test-all                   # all tests, requires Qdrant
go test -run TestMatchPattern ./internal/ingest/   # focused pkg test

# Quick ops (server must be on :8080)
make ingest                     # POST /ingest
make search                     # POST /search "secant formula"
make generate                   # POST /search with generate=true (streams LLM)
make reset                      # DELETE Qdrant collection (REST :6333)

make check                      # verify prereqs: docker, go, python3, ollama
```

## Architecture

Single Go binary at `cmd/server/main.go`. Wiring in `internal/httpserver/server.go`.

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

**`internal/middleware/`** — stdlib chain. `Chain()` applies outermost-first: `Recovery→RequestID→Timeout`.

**`services/`** — Python sidecars (each has own Dockerfile): `reranker/` (:5002), `docling/` (PDF→MD).

## Key rules

- Domain packages must NOT import `httpserver/` or `middleware/`
- Retry logic lives in `Pipeline`, never in `Embedder`/`Store`
- Chunk IDs = UUIDv5 over `filePath:lineStart:chunkIndex` — deterministic upserts, no duplicates
- Config: `config/config.yaml` → `config/config.go applyEnv()` overrides. Known env vars: `QDRANT_ADDR`, `OLLAMA_ADDR`, `RERANKER_ADDR`, `LOGGER_LEVEL`, `SEMANTIC_CACHE_THRESHOLD`, `QDRANT_COLLECTION`, `EMBEDDER_API_KEY`
- Source dirs set via `source.paths` in config (list of paths); no env override for source dirs

## Addresses: local vs Docker

`make dev` (scripts/dev-local.sh) overrides to localhost for host-side server. Inside Docker: Qdrant `qdrant:6334`, reranker `reranker:5002`. Ollama always at `localhost:11434` (or `host.docker.internal:11434` from containers).

## Features gated by config

| Feature | Config key | Requires |
|---------|-----------|----------|
| Answer generation | `generator.enabled` (on by default) | Ollama LLM; `POST /search` with `{"generate": true}` |
| Semantic cache | `semantic_cache.enabled` (on by default) | None (reuses Qdrant) |
| Reranker | `reranker.enabled` (on by default) | Reranker sidecar |

`ollama_addr` defaults to `embedder.ollama_addr` when empty for generator.

## Eval CLI

Two binaries: `cmd/server` (the service) and `cmd/eval` (retrieval + RAGAS eval). Eval requires an explicit `-golden` path:
```bash
make eval golden=my-golden.yaml
make eval-rag golden=my-golden.yaml
```

Eval does NOT ingest data — it queries an already-populated Qdrant collection. The golden set YAML uses suffix-based path matching (`math/trig.md` matches stored `gitbook/math/trig.md`). No golden set ships with the repo; you must provide one.
