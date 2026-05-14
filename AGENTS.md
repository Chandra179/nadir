# AGENTS.md

## Commands

```bash
# Build
go build ./cmd/server

# Run (config from config/config.yaml; env overrides in config/config.go applyEnv)
make run                        # go run ./cmd/server

# All-in-one dev: Qdrant + sidecars + Prometheus + Grafana + server + ingest
make dev                        # runs scripts/dev-local.sh (overrides addresses to localhost)

# Vendor deps (committed to repo — always run after adding imports)
make vendor                     # go mod tidy && go mod vendor

# Test — requires Qdrant via testcontainers (Docker)
go test ./...                   # eval tests pull qdrant/qdrant:latest on first run

# Focused tests
go test -run TestMatchPattern ./internal/pkb/
go test -run TestSearchEval ./internal/pkb/          # eval (needs Qdrant, Ollama)
EVAL_STORE=container go test -run TestSearchEval ...  # ephemeral Qdrant, full re-ingest
EVAL_STORE=live EVAL_JUDGE=llm go test -run TestSearchEval ...  # live Qdrant, LLM judge

# Quick ops (assumes server on :8080)
make ingest                     # POST /ingest
make search                     # POST /search with sample query
make generate                   # POST /search with generate=true (streams LLM answer)
make reset                      # DELETE Qdrant collection (REST API on :6333)

# Generate eval qrels (before eval-fresh)
go run ./cmd/gen-qrels

make check                      # verify prereqs: docker, go, python3, ollama
```

## Architecture

Single Go binary at `cmd/server/main.go`. Wires everything in `internal/httpserver/server.go`.

```
POST /ingest  → IngestHandler → FileLister → Fetcher → Pipeline (chunk → embed → upsert)
POST /search  → SearchHandler → [HyDE → Embedder → HybridSearch → Reranker → ChunkFilter → Generator]
GET  /healthz → 200
GET  /metrics → Prometheus (OTel)
```

**`internal/pkb/`** — core engine. All new domain logic belongs here.
- `Chunker`: `RecursiveChunker` (heading→paragraph→sentence→word) or `SentenceWindowChunker`
- `Embedder`: `OllamaEmbedder` (768-dim `nomic-embed-text`); swappable via interface
- `Store`: `QdrantStore` via gRPC; `HybridSearch` combines dense vector + BM25 sparse via RRF
- `Pipeline`: chunks → embeds (exponential backoff retry) → upserts; SHA-based dedup
- `FileLister`: walks KB dirs, glob-ignore filter

**`internal/middleware/`** — stdlib-only chain. `Chain()` applies outermost-first: `Recovery → RequestID → Timeout`.

**`pkg/otel/`** — Prometheus OTel metrics provider.

**`services/`** — Python sidecars: `splade/` (sparse scoring, :5001), `reranker/` (cross-encoder, :5002), `docling/` (PDF→MD).

## Key rules

- **Dependency flow inward**: `internal/pkb/` must NOT import `httpserver` or `middleware`.
- **Retry logic** lives in `Pipeline`, never in `Embedder` or `Store`.
- **Chunk IDs** = FNV hash of `filePath+lineStart` — known low-priority collision risk across files.
- **Config**: YAML first, then `config/config.go applyEnv()` overrides. All known env overrides listed there.

## Addresses: local vs Docker

`make dev` overrides Docker-internal hostnames to localhost so the Go server (running on the host) can reach Docker services. When running the server inside Docker (`docker compose up app`), Qdrant is `qdrant:6334` and services are `splade:5001` / `reranker:5002`. Ollama always runs on the host at `localhost:11434` (or `http://host.docker.internal:11434` from inside containers).

## Prerequisites

- Docker (for Qdrant + sidecars)
- Ollama running locally with `nomic-embed-text` pulled
- Python 3.10+ (for SPLADE/reranker/docling sidecars if run outside Docker)
- PDF conversion (docling) requires `make docling-install` once

## Features gated by config

| Feature | Config key | Requires |
|---------|-----------|----------|
| HyDE query expansion | `hyde.enabled` | Ollama LLM (e.g. `gemma3:1b`) |
| Chunk filter (LLM post-retrieval) | `chunk_filter.enabled` | Ollama LLM |
| Answer generation | `generator.enabled` (on by default) | Ollama LLM; `POST /search` with `{"generate": true}` |
| Semantic cache | `semantic_cache.enabled` (on by default) | None (reuses Qdrant) |
| Reranker | `reranker.enabled` (on by default) | Reranker sidecar |
| SPLADE sparse scorer | `sparse_scorer.provider: splade` | SPLADE sidecar |

`ollama_addr` defaults to `embedder.ollama_addr` when empty for all sub-features (HyDE, generator, chunk filter, EvalOps).
