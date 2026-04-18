# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Vendor dependencies
make vendor          # go mod tidy && go mod vendor

# Build
go build ./cmd/http

# Run
go run ./cmd/http

# Test
go test ./...
go test ./internal/pkb/...   # single package

# Docker (includes Qdrant)
docker compose up --build
```

Config loads from `config/config.yaml`. Secrets override via env: `GITHUB_WEBHOOK_SECRET`, `GITHUB_API_TOKEN`, `OPENAI_API_KEY`.

## Architecture

Single HTTP binary (`cmd/http/main.go`) — no gRPC entrypoint currently active.

**Request flow:** `main` → `httpserver.Server` wires middleware chain + handlers → serves two routes:
- `POST /webhook/github` → `WebhookHandler` → `Pipeline.Ingest/Delete`
- `POST /search` → `SearchHandler` → `Embedder` + `Store.Search`

**`internal/pkb/` — core PKB engine (interfaces + orchestration):**
- `Chunker` interface: splits markdown into `DocumentChunk` (text + source pointer)
- `Embedder` interface: `Embed(ctx, text) []float32` — swappable OpenAI / Ollama
- `Store` interface: Qdrant-backed upsert/delete/search over `ScoredChunk`
- `Fetcher` interface: pulls raw file content from GitHub REST API
- `Pipeline`: chunks → embeds (with exponential backoff retry) → upserts; called async from webhook handler
- Concrete implementations of all four interfaces are **not yet built** — `httpserver.Server` passes `nil` until they exist

**`internal/middleware/`:** stdlib-only chain (`Recovery → RequestID → Timeout`). `Chain()` applies outermost-first. Add new middleware by appending to the `globalChain` call in `httpserver.Server`.

**`config/`:** Single `Config` struct decoded from YAML, then env vars overlay sensitive fields. All subsystem configs (retry, embedder, chunker, qdrant) live here.

## Key design rules

- Modules must not import `httpserver` or `middleware` — dependency flows inward only.
- New capabilities go in `internal/pkb/` as a new file implementing one of the four interfaces.
- Retry logic lives in `Pipeline`, not in `Embedder`/`Store` implementations.
