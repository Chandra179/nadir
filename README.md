# nadir

Personal knowledge base (PKB) search engine. Ingests markdown notes, chunks + embeds them locally, stores in Qdrant, serves hybrid semantic+keyword search over HTTP.

## Prerequisites

- Docker + Docker Compose
- Go 1.26+
- Python 3.10+
- [Ollama](https://ollama.com) running locally with `nomic-embed-text` pulled

```bash
ollama pull nomic-embed-text
```

## Quick start

```bash
make dev
```

Starts everything in one shot: Qdrant, SPLADE sidecar, reranker sidecar, Prometheus, Grafana, Go server, then runs ingest automatically.

Set your notes path first — see [Config](#config).

## Run separately

If you want manual control over each component:

```bash
# 1. Start Docker services (Qdrant, sidecars, monitoring)
docker compose up -d qdrant splade reranker prometheus grafana

# 2. Start Go server
make run

# 3. Ingest notes
make ingest
```

## Scaling

Local setup handles low-to-moderate concurrent users fine (Go server + Docker sidecars on one host). For higher concurrency or availability requirements, consider: load balancer in front of multiple server instances, dedicated Qdrant node, GPU-backed embedding/reranker, or container orchestration (k8s).

## Config

Config file: `config/config.yaml`

**Notes path** — two ways to set:
- Edit `knowledge_base.path` in `config/config.yaml`
- Env var: `NOTES_PATH=<path> make run`

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
| GET | `/healthz` | Health check |

## Architecture

```
POST /ingest → FileLister → Fetcher → Pipeline
                                         ├── Chunker (heading→paragraph→sentence→word)
                                         ├── Embedder (Ollama)
                                         └── Store.Upsert (Qdrant)

POST /search → [HyDE] → Embedder → Store.HybridSearch (dense + BM25 → RRF)
                                         └── [Reranker] → [ChunkFilter] → response
```

Core logic: `internal/pkb/`. SHA-based dedup — unchanged files skip re-embedding.

## Dev commands

```bash
make vendor      # go mod tidy && go mod vendor
go build ./cmd/server
go test ./...
make reset       # delete Qdrant collection
make check       # verify prereqs (docker, go, python3, ollama)
```

## Monitoring

Grafana: `http://localhost:3000` (default `admin/admin`)  
Prometheus: `http://localhost:9090`

## PDF ingestion

Drop PDFs in `pdfs/raw/`. On `make dev` / `make prod`, docling converts them to markdown in `pdfs/converted/`, picked up on next ingest.

Manual conversion:
```bash
make docling-install   # one-time
make docling
make ingest
```
