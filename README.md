# nadir

Personal knowledge base (PKB) search engine. Ingests markdown notes, chunks + embeds them locally, stores in Qdrant, serves hybrid semantic+keyword search over HTTP.

## Prerequisites

| Tool | Required? | Purpose |
|------|-----------|---------|
| Docker + Docker Compose | **Required** | Qdrant, sidecars (SPLADE, reranker), monitoring |
| Go 1.26+ | **Required** | Server + CLI |
| Python 3.10+ | **Required** | SPLADE sidecar, reranker sidecar, PDF conversion |
| [Ollama](https://ollama.com) | **Required** | Embeddings (`nomic-embed-text`) and optional LLM features |

```bash
ollama pull nomic-embed-text
```

## Quick start

```bash
# 1. Install sidecar dependencies (one-time)
make splade-install
make reranker-install
make docling-install

# 2. Start everything + ingest
make dev
```

`make dev` starts Qdrant, SPLADE sidecar, reranker sidecar, Prometheus, Grafana, Go server, then runs ingest automatically.

Set your notes path first — see [Config](#config).

### Verify it works

```bash
# After make dev completes, test search:
make search
# Expected: JSON response with "results" array containing scored chunks.
```

If you get connection errors, see [Troubleshooting](#troubleshooting).

## Run separately

```bash
# 1. Start Docker services (Qdrant, sidecars, monitoring)
docker compose up -d qdrant splade reranker prometheus

# 2. Start Go server
make run

# 3. Ingest notes
make ingest
```

## Config

Config file: `config/config.yaml`. All keys with defaults are shown there — edit directly.

### Minimal config to get started

Only one thing to change: your notes path.

```yaml
# config/config.yaml
knowledge_base:
  path: "~/notes"  # your markdown notes directory
```

Everything else has sensible defaults. For a full reference of every knob, open `config/config.yaml`.

### Notes path

Two ways to set:

- Edit `knowledge_base.path` in `config/config.yaml` (single directory)
- Add extra dirs under `knowledge_base.paths` (merged with `path`)
- Env var override: `NOTES_PATH=<path> make run`

### Env vars

All env vars are defined in `docker-compose.yml` under each service's `environment:` block. Edit them there — do **not** set them in `.env` or shell exports for prod.

Key vars:

| Var | Default (docker-compose) | Purpose |
|-----|--------------------------|---------|
| `QDRANT_ADDR` | `qdrant:6334` | Qdrant gRPC address |
| `OLLAMA_ADDR` | `http://host.docker.internal:11434` | Ollama host |
| `SPLADE_ADDR` | `http://splade:5001` | SPLADE sidecar |
| `RERANKER_ADDR` | `http://reranker:5002` | Reranker sidecar |
| `QDRANT_COLLECTION` | `pkb_chunks` | Qdrant collection name |
| `LOGGER_LEVEL` | `prod` | `dev` or `prod` |

> `make dev` overrides these to `localhost:*` so Go server on host can reach Docker services.

### Features disabled by default

Some features in `config/config.yaml` are off by default — enable when needed:

| Feature | Config key | Notes |
|---------|-----------|-------|
| HyDE query expansion | `hyde.enabled: false` | Requires Ollama LLM (e.g. `gemma3:1b`); uses `hyde.model` |
| Chunk filtering | `chunk_filter.enabled: false` | Post-retrieval LLM filter; +10pp PopQA; requires Ollama LLM |
| Answer generation | `generator.enabled: true` | Already on; POST `/search` with `"generate": true` |
| Semantic cache | `semantic_cache.enabled: true` | Already on; reuses Qdrant |
| Reranker | `reranker.enabled: true` | Already on; requires reranker sidecar |

## Routes

| Method | Path | Description |
|--------|------|-------------|
| POST | `/ingest` | Walk notes dir, chunk+embed new/changed files |
| POST | `/search` | Hybrid semantic search over embedded chunks |

## Architecture

```
POST /ingest → FileLister → Fetcher → Pipeline
                                         ├── Chunker (heading→paragraph→sentence→word)
                                         ├── Embedder (Ollama)
                                         └── Store.Upsert (Qdrant)

POST /search → [HyDE] → Embedder → Store.HybridSearch (dense + BM25 → RRF)
                                         └── [Reranker] → [ChunkFilter] → response
```

## Run tests

### Unit tests (no Docker required)

```bash
make test        # unit tests only; runs in seconds
```

Tests without infrastructure dependencies: chunk matching, ignore patterns, HyDE vector ops.

## PDF ingestion

Drop PDFs in `pdfs/raw/`. On `make dev`, docling converts them to markdown in `pdfs/converted/`, picked up on next ingest.

```bash
make docling-install   # one-time: install Python deps
make docling            # convert PDFs → markdown
make ingest             # ingest converted markdown
```

## Troubleshooting

### `make dev` fails with connection errors

Ensure Docker is running and no other services occupy ports 6333/6334/5001/5002/8080. Run `make reset` to clear stale Qdrant state and retry.

### Ollama connection refused

```bash
# Check Ollama is running
curl http://localhost:11434/api/tags

# If not, start it manually
ollama serve
```

### "model not found" during ingest/search

```bash
ollama pull nomic-embed-text
# If using HyDE, generator, or chunk filter:
ollama pull gemma3:1b
```

### Qdrant gRPC errors

The server uses gRPC on port 6334 (not the REST API on 6333). If you see gRPC dial errors, verify `QDRANT_ADDR` matches your Qdrant container's gRPC port.

### Port already in use

```bash
# Find what's using a port
lsof -i :8080
# Stop conflicting services, or change http.addr in config/config.yaml
```