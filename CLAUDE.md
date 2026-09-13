# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Agent skills

### Issue tracker

Issues and PRDs for this GitHub repository are published as GitHub issues. See `docs/agents/issue-tracker.md`.

### Triage labels

Use the canonical labels `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, and `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

This is a single-context repository with one root `CONTEXT.md` and system decisions in `docs/adr/`. See `docs/agents/domain.md`.

## What this is

Nadir is a semantic document search engine: ingests markdown/PDF/text files, chunks + embeds them locally (Ollama), stores vectors in Qdrant, and serves hybrid semantic+keyword search over HTTP, with optional cross-encoder reranking and LLM answer generation. The API binary is `cmd/api/main.go` and the evaluator is `cmd/evaluator/main.go`. Two Python sidecars live under `sidecars/` (reranker, document-converter PDF→MD).

## Commands

```bash
# Build
go build ./cmd/api

# Vendor deps (NOT committed — gitignored; run after adding/changing imports)
go mod tidy && go mod vendor

# Dev: Qdrant + sidecars + server + auto-ingest
./scripts/local.sh              # addrs come from config/config.yaml (localhost)

# Run standalone (config/config.yaml, .env sourced)
go run ./cmd/api

# Tests
go test -short -count=1 ./internal/platform/configuration ./cmd/... ./internal/... # unit tests, no Docker
go test -count=1 ./internal/platform/configuration ./cmd/... ./internal/...       # all Go tests
go test -run TestMatchPattern ./internal/knowledge/indexing/   # focused package test

# Quick ops (server must be on :8100)
curl -X POST localhost:8100/api/v1/documents
curl -X POST localhost:8100/api/v1/turns -H 'content-type: application/json' -d '{"query":"secant formula","generate":false}'
curl -X POST localhost:8100/api/v1/documents/reset                    # safely reset the Document collection
```

> The Makefile provides `run`, `test`, `race`, `vet`, `build`, and `check` targets. The Go checks use explicit package scopes so a local Python `venv/` is not discovered as a Go package. The retrieval-quality evaluator lives at `cmd/evaluator`; `test/evaluation/` contains only committed evaluation data and reports.

## Architecture

```
POST /api/v1/documents → IngestHandler → knowledge/indexing (walk + SHA dedup → chunk→embed→replace)
POST /api/v1/turns → StartTurnHandler → chat.Service.StartTurn (session mint or in-place edit prune → retrieve → supervised generation)
GET  /api/v1/turns/:id/events → bounded replayable SSE event log
POST /api/v1/turns/:id/cancel → cancel generation and persist the partial answer
GET  /api/v1/health → 200
```

Shared retrieval/indexing wiring lives in `internal/platform/runtime/`; HTTP
process wiring and lifecycle live in `internal/platform/server/server.go`
(entrypoint `server.Server(ctx, cfg)`, called from `cmd/api/main.go`). HTTP
handlers and route registration live in `internal/transport/http/`.

**Bounded contexts (under `internal/`):**
- `knowledge/` — Document intake and the Indexing pass: normalize, chunk,
  enrich, embed, deduplicate, and versioned publication.
- `retrieval/` — query rewriting, fragmentation, hybrid dense/BM25 search,
  RRF, cache lookup, reranking, and context selection.
- `conversation/` — Sessions, Chat turns, prompt construction, generation
  supervision, bounded event retention, cancellation, edit/prune, and history.
- `evaluation/` — golden-set Retrieval evaluation and reports.

**Transport and infrastructure:**
- `transport/http/` — versioned JSON/SSE transport and route registration.
- `adapters/qdrant/` — Qdrant clients and persistence Adapters.
- `adapters/ollama/` — embedding, enrichment, generation, and rewriting
  Adapters.
- `adapters/reranker/` and `adapters/docling/` — sidecar Adapters.
- `platform/` — configuration, logging, observability, HTTP middleware, shared
  runtime composition, and server lifecycle.

The React/TypeScript/Tailwind dashboard is a separate package under
`web/dashboard`; Python sidecars remain under `sidecars/` as independent
processes.

`sidecars/` — Python sidecars (each has own Dockerfile): `reranker/` (:5002), optional `document-converter/` (:5003, PDF→Markdown HTTP intake).

`internal/adapters/qdrant/` — shared Qdrant clients, dense collection setup,
payload codecs, point-ID decoding, and persistence Adapters; document, history,
and semantic-cache lifecycle rules remain separate.

### Request pipeline details

1. **Document intake & chunk** — Markdown is already normalized; optional PDFs are converted by Docling before sentence-based chunking for precise citations; chunk size/overlap/strategy are configurable.
2. **Embed** — each chunk is prefixed with its file path + heading before embedding, anchoring the vector in document structure without altering the stored text.
3. **Semantic cache** — query embedding is checked against a dedicated Qdrant collection by cosine similarity before search; filtered searches bypass it, and cache entries are versioned by the embedding configuration.
4. **Search** — dense (cosine nearest-neighbor) and sparse (BM25-style term vectors, IDF-weighted server-side by Qdrant) legs run as native Qdrant prefetches and fuse via Reciprocal Rank Fusion (RRF) in a single query. Long queries are split into sentence fragments, embedded in one batch call, searched in parallel, then deduped/re-sorted with a per-file cap for diversity. The search use-case accepts an optional filter (`file_path`, `header`, `source_sha`) for exact-match keyword scoping.
5. **Re-rank** — top-N candidates re-scored by the cross-encoder reranker sidecar, if enabled/available.
6. **Generate** — chunks reordered by the "lost in the middle" heuristic (most relevant at both ends of context, least relevant in the middle) before being placed in the system prompt; Ollama streams the answer token by token through the bounded broker.

## Key rules

- Domain contexts must NOT import `internal/transport/http/`,
  `internal/platform/server/`, `internal/platform/httpmiddleware/`, or
  frontend code.
- Retry logic lives in `Pipeline`, never in `Embedder`/`Store`
- Chunk IDs = UUIDv5 over `filePath:lineStart:chunkIndex` — deterministic upserts, no duplicates
- Config: `config/config.yaml` → `internal/platform/configuration/config.go` `applyEnv()` overrides. Known env vars include `QDRANT_ADDR`, `QDRANT_COLLECTION`, `OLLAMA_ADDR`, `GENERATOR_ADDR`, `GENERATOR_MODEL`, `EMBEDDER_API_KEY`, `SOURCE_PATHS`, `SOURCE_IGNORE_PATTERNS`, `RERANKER_ADDR`, `RERANKER_ENABLED`, `RERANKER_MODEL`, `LOGGER_LEVEL`, `SEMANTIC_CACHE_THRESHOLD`, `HYPE_ENABLED`, `HYPE_ADDR`, `HYPE_MODEL`, `CONTEXTUAL_ENABLED`, `CONTEXTUAL_ADDR`, `CONTEXTUAL_MODEL`, `REWRITE_ENABLED`, `REWRITE_ADDR`, `REWRITE_MODEL`, `REWRITE_TURNS`, `HISTORY_ENABLED`, `HISTORY_COLLECTION`, `DOCLING_ENABLED`, `DOCLING_ADDR`
- Source dirs are configured by `source.paths`; `SOURCE_PATHS` is a comma-separated override used by Compose and container deployments
- External Ollama/sidecar request timeouts are configured per role in `config/config.yaml`; constructors retain defaults only for direct package tests

## Addresses: local vs Docker

`config/config.yaml` defaults to `localhost` addresses for the host-side server (`./scripts/local.sh` runs it as-is). The base Compose stack is CPU-safe for Linux, Windows Docker Desktop, and macOS; it uses Qdrant `qdrant:6334`, reranker `reranker:5002`, and Ollama `host.docker.internal:11434`. Layer `deploy/compose/docker-compose.gpu.yml` only on Linux or Windows WSL2 with NVIDIA support.

## Features gated by config

| Feature | Config key | Requires |
|---------|-----------|----------|
| Answer generation | `generator.enabled` (on by default) | Ollama LLM; dashboard or `POST /api/v1/turns` with `generate=true` |
| Semantic cache | `semantic_cache.enabled` (on by default) | None (reuses Qdrant) |
| Reranker | `reranker.enabled` (on by default) | Reranker sidecar |
| Chat history | `history.enabled` (on by default) | None (reuses Qdrant); persists chat sessions/turns to a dedicated collection, browsable via the dashboard and `/api/v1/sessions/:id` |
| PDF document intake | `docling.enabled` (off by default) | Docling sidecar; source PDFs are converted before indexing |

Enabled LLM roles must declare their own `ollama_addr` and `model`; generator,
rewriter, HyPE, and contextual enrichment do not inherit another role's
endpoint or model.

## Sample data

A sample set lives at `samples/` (4 math markdown files). Add your own dirs to `source.paths` in `config/config.yaml`. Only new/changed files are processed (SHA-256 dedup).

## Prerequisites

Docker + Docker Compose (Qdrant, reranker sidecar), Go 1.26+, Python 3.10+ (reranker sidecar, PDF conversion), [Ollama](https://ollama.com) (`nomic-embed-text` for embeddings, `gemma3:1b` for generation).
