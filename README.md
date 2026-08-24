# nadir

Semantic document search engine. Ingests text files, chunks + embeds them locally, stores in Qdrant, serves hybrid semantic+keyword search over HTTP, with optional cross-encoder reranking and LLM answer generation.

## Prerequisites

| Tool | Required? | Purpose |
|------|-----------|---------|
| Docker + Docker Compose | **Required** | Qdrant, reranker sidecar |
| Go 1.26+ | **Required** | Server + CLI |
| Python 3.10+ | **Required** | Reranker sidecar, PDF conversion |
| [Ollama](https://ollama.com) | **Required** | Embeddings (`nomic-embed-text`) and optional LLM features |

```bash
ollama pull nomic-embed-text
ollama pull gemma3:1b   # for answer generation
```

## Quick start

### 1. Configure your data source

Edit `config/config.yaml` → `source.paths` to point at your markdown files:

```yaml
source:
  paths:
    - "samples"           # ships with sample math docs
    - "~/my-documents"    # your own data
```

### 2. Start everything

```bash
./scripts/local.sh
```

This starts Qdrant + reranker, runs the Go server, ingests all source files, and blocks on the server.

### 3. Test search

```bash
curl -X POST localhost:8100/retrieval/search --data-urlencode "query=secant formula"
```

Returns an HTML fragment (the chat UI's turn card) plus an `X-Nadir-Session-Id`
header for follow-up turns. Add `-F generate=true` to include an LLM answer.

### 4. Include LLM answer generation

Pass `generate=true` to run answer generation over the retrieved chunks:

```bash
curl -X POST localhost:8100/retrieval/search \
  --data-urlencode "query=secant formula" -F generate=true
```

## Source data

The server reads markdown files from directories listed in `config.yaml` → `source.paths`. Each source path is walked recursively; files matching `ingest.ignore_patterns` are skipped.

A sample set is included at `samples/` (4 math files). To use your own data:

```yaml
# config/config.yaml
source:
  paths:
    - "/path/to/your/docs"
    - "/another/directory"
```

Then run `./scripts/local.sh` again (or `curl -X POST localhost:8100/ingest` on a running server). Only new/changed files are processed (SHA-256 dedup).

## Run separately

```bash
# 1. Start Docker services (Qdrant + reranker)
docker compose up -d qdrant reranker

# 2. Start Go server
go run ./cmd/server

# 3. Ingest documents
curl -X POST localhost:8100/ingest
```

## Config

Config file: `config/config.yaml`. All keys with defaults are shown there — edit directly.

### Minimal config

```yaml
# config/config.yaml
source:
  paths:
    - "~/documents"
```

Everything else has sensible defaults. For a full reference of every knob, open `config/config.yaml`.

### Env vars

| Var | Default (docker-compose) | Purpose |
|-----|--------------------------|---------|
| `QDRANT_ADDR` | `qdrant:6334` | Qdrant gRPC address |
| `QDRANT_COLLECTION` | `documents_chunks` | Qdrant collection name |
| `OLLAMA_ADDR` | `http://host.docker.internal:11434` | Ollama host |
| `EMBEDDER_API_KEY` | — | Embedder API key, if required |
| `RERANKER_ADDR` | `http://reranker:5002` | Reranker sidecar |
| `RERANKER_ENABLED` | — | `true`/`1` to force-enable the reranker |
| `LOGGER_LEVEL` | `prod` | `dev` or `prod` |
| `SEMANTIC_CACHE_THRESHOLD` | — | Cosine similarity threshold for a cache hit |

> `./scripts/local.sh` runs the server against `config/config.yaml`'s `localhost:*` addresses directly — no env overrides needed. `docker-compose.yml` sets these env vars on the containerized `app` service to reach the other Docker services.

## Routes

| Method | Path | Description |
|--------|------|-------------|
| POST | `/ingest` | Ingest uploaded files: chunk+embed new/changed ones (SHA-256 dedup) |
| POST | `/store/reset` | Drop and recreate the Qdrant collection |
| GET | `/retrieval` | Chat UI |
| POST | `/retrieval/search` | One chat turn: retrieve → (optional) generate → persist |
| GET | `/history/sessions` | Recent chat sessions (sidebar) |
| GET | `/history/sessions/:id` | Replay a past session |
| GET | `/settings` | Effective runtime configuration panel |
| GET | `/healthz` | Health check |

## Architecture

```
POST /ingest → ingest.Service (SHA dedup) → Pipeline
                                 ├── Chunker (recursive / sentence-window)
                                 ├── Embedder (Ollama)
                                 └── Store.Upsert (Qdrant)

POST /retrieval/search → chat.Service.Ask
                 ├── search.Service → Embedder → hybrid search (dense + sparse → RRF) → [Reranker]
                 ├── [Generator] buffered answer over retrieved chunks
                 └── History persist (detached)

## Run tests

### Unit tests (no Docker required)

```bash
go test -short -count=1 ./...   # unit tests only; runs in seconds
go test -count=1 ./...          # all tests (requires Qdrant)
```

## PDF ingestion

Docling converts PDFs to markdown for ingestion (`services/docling/main.py`).

```bash
pip install -r services/docling/requirements.txt   # one-time: install Python deps
python services/docling/main.py --input pdfs/raw --output pdfs/converted   # convert PDFs → markdown
curl -X POST localhost:8100/ingest                  # ingest converted markdown
```

## Troubleshooting

### `./scripts/local.sh` fails with connection errors

Ensure Docker is running and no other services occupy ports 6333/6334/5002/8100. Clear stale Qdrant state and retry:

```bash
curl -X DELETE localhost:6333/collections/documents_chunks
```

(or use the "Delete all" button in the ingestion dashboard at `/dashboard`, which does the same drop-and-recreate.)

### Ollama connection refused

```bash
curl http://localhost:11434/api/tags
ollama serve
```

### "model not found" during ingest/search

```bash
ollama pull nomic-embed-text
ollama pull gemma3:1b   # for answer generation
```

### Qdrant gRPC errors

The server uses gRPC on port 6334 (not the REST API on 6333). If you see gRPC dial errors, verify `QDRANT_ADDR` matches your Qdrant container's gRPC port.

### Port already in use

```bash
lsof -i :8100
# Change http.addr in config/config.yaml if needed
```
