# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Vendor dependencies
make vendor          # go mod tidy && go mod vendor

# Build
go build ./cmd/http

# Run (sources .env)
make run             # or: go run ./cmd/http

# Test
go test ./...
go test ./internal/pkb/...   # single package

# Docker (includes Qdrant)
docker compose up --build

# Quick ops
make ingest          # POST localhost:8080/ingest
make search          # POST localhost:8080/search with sample query
make d               # delete Qdrant collection
```

Config loads from `config/config.yaml`. Override notes path via env: `NOTES_PATH`.

## Architecture

Single HTTP binary (`cmd/http/main.go`).

**Request flow:** `main` → `httpserver.Server` wires middleware chain + handlers → serves two routes:
- `POST /ingest` → `IngestHandler` → `FileLister` → `Fetcher` → `Pipeline.IngestFile`
- `POST /search` → `SearchHandler` → `Embedder` + `Store.HybridSearch`

**`internal/pkb/` — core PKB engine:**
- `Chunker` (`RecursiveChunker`): Goldmark AST walk → splits by heading → paragraph → sentence → word; emits plain text (strips markdown syntax before embedding)
- `Embedder` (`OllamaEmbedder`): local Ollama (`nomic-embed-text`, 768-dim); swappable via interface
- `Store` (`QdrantStore`): Qdrant via gRPC; upsert/delete/search; `HybridSearch` combines dense vector + BM25 sparse via RRF (prefetch `topK*3` each modality)
- `Fetcher` (`LocalFetcher`): reads `.md` from local filesystem
- `FileLister` (`LocalFileLister`): walks `knowledge_base.path` dir, supports glob ignore patterns, returns `FileEntry{Path, SHA}`
- `Pipeline`: chunks → embeds (exponential backoff retry) → upserts; SHA-based dedup skips unchanged files

**`internal/middleware/`:** stdlib-only chain (`Recovery → RequestID → Timeout`). `Chain()` applies outermost-first.

**`config/`:** Single `Config` struct decoded from YAML. All subsystem configs (retry, embedder, chunker, qdrant) live here.

## Key design rules

- Modules must not import `httpserver` or `middleware` — dependency flows inward only.
- New capabilities go in `internal/pkb/` as a new file implementing one of the four interfaces.
- Retry logic lives in `Pipeline`, not in `Embedder`/`Store` implementations.
- Chunk IDs = FNV hash of `filePath+lineStart` — known collision risk across files (low urgency; fix: UUIDv5).
