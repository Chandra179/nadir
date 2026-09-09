# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Nadir is a semantic document search engine: ingests markdown/PDF/text files, chunks + embeds them locally (Ollama), stores vectors in Qdrant, and serves hybrid semantic+keyword search over HTTP, with optional cross-encoder reranking and LLM answer generation. Single Go binary at `cmd/server/main.go`. Two Python sidecars live under `services/` (reranker, docling PDF→MD).

## Commands

```bash
# Build
go build ./cmd/server

# Vendor deps (NOT committed — gitignored; run after adding/changing imports)
go mod tidy && go mod vendor

# Dev: Qdrant + sidecars + server + auto-ingest
./scripts/local.sh              # addrs come from config/config.yaml (localhost)

# Run standalone (config/config.yaml, .env sourced)
go run ./cmd/server

# Tests
go test -short -count=1 ./...              # unit tests only, no Docker
go test -count=1 ./...                     # all tests, requires Qdrant
go test -run TestMatchPattern ./internal/ingest/   # focused package test

# Quick ops (server must be on :8100)
curl -X POST localhost:8100/ingest
curl -X POST localhost:8100/retrieval/search --data-urlencode "query=secant formula"
curl -X DELETE localhost:6333/collections/documents_chunks   # reset Qdrant collection (REST :6333)
```

> **Note:** `Makefile` currently only defines a `run` target (`./scripts/local.sh`) — the `dev`/`test`/`ingest`/`search`/`generate`/`reset`/`vendor`/`docling*`/`reranker*`/`check` targets referenced in `AGENTS.md`/`README.md` were lost in a past commit that truncated the file. Use the raw commands above until the Makefile is restored. The `cmd/eval` retrieval/RAGAS CLI referenced in older docs no longer exists; `tests/eval/` contains only committed evaluation data and reports.

## Architecture

```
POST /ingest → IngestHandler → ingest.Service (walk + SHA dedup) → Pipeline (chunk→embed→upsert)
POST /retrieval/search → RetrievalSearchHandler → chat.Service.StartTurn (session mint → retrieve → supervised generation)
GET  /retrieval/turns/:id/events → bounded replayable SSE event log
POST /retrieval/turns/:id/cancel → cancel generation and persist the partial answer
GET  /healthz → 200
```

Wiring lives in `internal/server/server.go` (entrypoint `server.Server(ctx, cfg)`, called from `cmd/server/main.go`); HTTP handlers and route registration live in `internal/api/`.

**Domain packages (under `internal/`):**
- `chunker/` — `Chunker` interface, `Chunk` value type, `RecursiveChunker`, `SentenceWindowChunker`, `ContextualText`
- `embedder/` — `Embedder`, `BatchEmbedder` interfaces, `OllamaEmbedder`
- `store/` — document-corpus `Store` interface and Qdrant hybrid Adapter; retrieval-facing chunk/filter types live in `search`
- `ingest/` — document intake (`.md`, optional `.pdf` through Docling), SHA dedup, bounded indexing pass
- `search/` — Retrieval-owned request/result types; bounded multi-fragment hybrid search → rerank → semantic cache
- `generator/` — `Generator` interface, `OllamaGenerator`, `buildPrompt`, `lostInMiddleOrder`
- `reranker/` — `Reranker` interface, `HTTPReranker` (cross-encoder sidecar client)
- `cache/` — `SemanticCache` backed by a dedicated Qdrant collection
- `history/` — `History` interface, `Session`/`Turn` value types, serialized chat persistence backed by a dedicated Qdrant collection (see `history.enabled`)

`internal/api/` — HTTP transport grouped by ingest, retrieval/chat, history, and reset features; `NewRouter` registers the current routes on the gin engine.

`internal/server/` — `Server(ctx, cfg)`: builds dependencies, wires middleware, starts the gin engine.

`internal/middleware/` — gin middleware, registered outermost-first in `internal/server/server.go`: `Recovery→RequestID→Timeout→RequestLog`. `Timeout` (from `middleware.timeout` in config) bounds downstream Qdrant/Ollama calls; source sweeps and SSE turn streams are exempt.

`services/` — Python sidecars (each has own Dockerfile): `reranker/` (:5002), optional `docling/` (:5003, PDF→Markdown HTTP intake).

`internal/qdrantutil/` — shared Qdrant clients, dense collection setup, payload
codecs, and point-ID decoding; document, history, and semantic-cache
lifecycle rules remain separate.

### Request pipeline details

1. **Document intake & chunk** — Markdown is already normalized; optional PDFs are converted by Docling before sentence-based chunking for precise citations; chunk size/overlap/strategy are configurable.
2. **Embed** — each chunk is prefixed with its file path + heading before embedding, anchoring the vector in document structure without altering the stored text.
3. **Semantic cache** — query embedding is checked against a dedicated Qdrant collection by cosine similarity before search; filtered searches bypass it, and cache entries are versioned by the embedding configuration.
4. **Search** — dense (cosine nearest-neighbor) and sparse (BM25-style term vectors, IDF-weighted server-side by Qdrant) legs run as native Qdrant prefetches and fuse via Reciprocal Rank Fusion (RRF) in a single query. Long queries are split into sentence fragments, embedded in one batch call, searched in parallel, then deduped/re-sorted with a per-file cap for diversity. The search use-case accepts an optional filter (`file_path`, `header`, `source_sha`) for exact-match keyword scoping.
5. **Re-rank** — top-N candidates re-scored by the cross-encoder reranker sidecar, if enabled/available.
6. **Generate** — chunks reordered by the "lost in the middle" heuristic (most relevant at both ends of context, least relevant in the middle) before being placed in the system prompt; Ollama streams the answer token by token through the bounded broker.

## Key rules

- Domain packages must NOT import `internal/api/`, `internal/server/`, or `internal/middleware/`
- Retry logic lives in `Pipeline`, never in `Embedder`/`Store`
- Chunk IDs = UUIDv5 over `filePath:lineStart:chunkIndex` — deterministic upserts, no duplicates
- Config: `config/config.yaml` → `config/config.go applyEnv()` overrides. Known env vars include `QDRANT_ADDR`, `QDRANT_COLLECTION`, `OLLAMA_ADDR`, `EMBEDDER_API_KEY`, `SOURCE_PATHS`, `SOURCE_IGNORE_PATTERNS`, `RERANKER_ADDR`, `RERANKER_ENABLED`, `LOGGER_LEVEL`, `SEMANTIC_CACHE_THRESHOLD`, `HISTORY_ENABLED`, `HISTORY_COLLECTION`, `DOCLING_ENABLED`, `DOCLING_ADDR`
- Source dirs are configured by `source.paths`; `SOURCE_PATHS` is a comma-separated override used by Compose and container deployments
- External Ollama/sidecar request timeouts are configured per role in `config/config.yaml`; constructors retain defaults only for direct package tests

## Addresses: local vs Docker

`config/config.yaml` defaults to `localhost` addresses for the host-side server (`./scripts/local.sh` runs it as-is). The base Compose stack is CPU-safe for Linux, Windows Docker Desktop, and macOS; it uses Qdrant `qdrant:6334`, reranker `reranker:5002`, and Ollama `host.docker.internal:11434`. Layer `docker-compose.gpu.yml` only on Linux or Windows WSL2 with NVIDIA support.

## Features gated by config

| Feature | Config key | Requires |
|---------|-----------|----------|
| Answer generation | `generator.enabled` (on by default) | Ollama LLM; chat UI or `POST /retrieval/search` with `generate=on` |
| Semantic cache | `semantic_cache.enabled` (on by default) | None (reuses Qdrant) |
| Reranker | `reranker.enabled` (on by default) | Reranker sidecar |
| Chat history | `history.enabled` (on by default) | None (reuses Qdrant); persists `/retrieval` chat sessions/turns to a dedicated collection, browsable via the sidebar and `/history/sessions/:id` |
| PDF document intake | `docling.enabled` (off by default) | Docling sidecar; source PDFs are converted before indexing |

`ollama_addr` defaults to `embedder.ollama_addr` when empty for generator.

## Sample data

A sample set lives at `samples/` (4 math markdown files). Add your own dirs to `source.paths` in `config/config.yaml`. Only new/changed files are processed (SHA-256 dedup).

## Prerequisites

Docker + Docker Compose (Qdrant, reranker sidecar), Go 1.26+, Python 3.10+ (reranker sidecar, PDF conversion), [Ollama](https://ollama.com) (`nomic-embed-text` for embeddings, `gemma3:1b` for generation).
