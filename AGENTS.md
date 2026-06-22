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
go test -run TestMatchPattern ./internal/engine/   # focused pkg test

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
POST /ingest → IngestHandler → FileLister → Fetcher → Pipeline (chunk→embed→upsert)
POST /search → SearchHandler → Embedder → HybridSearch → [Reranker] → [ChunkFilter] → [Generator]
GET  /healthz → 200
```

**`internal/engine/`** — core engine. All new domain logic belongs here.
- `RecursiveChunker` (heading→paragraph→sentence→word) or `SentenceWindowChunker`
- `OllamaEmbedder` (768-dim `nomic-embed-text`); swappable via interface
- `QdrantStore` via gRPC; `HybridSearch` = dense vector + BM25 sparse → RRF
- `Pipeline`: chunks → embeds (exponential backoff retry) → upserts; SHA-based dedup

**`internal/middleware/`** — stdlib chain. `Chain()` applies outermost-first: `Recovery→RequestID→Timeout`.

**`services/`** — Python sidecars (each has own Dockerfile): `splade/` (:5001), `reranker/` (:5002), `docling/` (PDF→MD).

## Key rules

- `internal/engine/` must NOT import `httpserver` or `middleware`
- Retry logic lives in `Pipeline`, never in `Embedder`/`Store`
- Chunk IDs = UUIDv5 over `filePath:lineStart:chunkIndex` — deterministic, no collisions within the namespace
- Config: `config/config.yaml` → `config/config.go applyEnv()` overrides. Known env vars: `SOURCE_PATH`, `QDRANT_ADDR`, `OLLAMA_ADDR`, `SPLADE_ADDR`, `RERANKER_ADDR`, `LOGGER_LEVEL`, `SEMANTIC_CACHE_THRESHOLD`

## Addresses: local vs Docker

`make dev` (scripts/dev-local.sh) overrides to localhost for host-side server. Inside Docker: Qdrant `qdrant:6334`, splade `splade:5001`, reranker `reranker:5002`. Ollama always at `localhost:11434` (or `host.docker.internal:11434` from containers).

## Features gated by config

| Feature | Config key | Requires |
|---------|-----------|----------|
| Chunk filter | `chunk_filter.enabled` | Ollama LLM |
| Answer generation | `generator.enabled` (on by default) | Ollama LLM; `POST /search` with `{"generate": true}` |
| Semantic cache | `semantic_cache.enabled` (on by default) | None (reuses Qdrant) |
| Reranker | `reranker.enabled` (on by default) | Reranker sidecar |
| SPLADE sparse scorer | `sparse_scorer.provider: splade` | SPLADE sidecar |

`ollama_addr` defaults to `embedder.ollama_addr` when empty for generator, chunk filter.
