# nadir

Semantic document search engine. Ingests text files, chunks + embeds them locally, stores in Qdrant, serves hybrid semantic+keyword search over HTTP. Includes eval harness for measuring retrieval quality and RAGAS metrics.

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
make dev
```

This starts Qdrant + reranker, runs the Go server, ingests all source files, and blocks on the server.

### 3. Test search

```bash
make search
# or with a custom query:
curl -X POST localhost:8080/search \
  -H "Content-Type: application/json" \
  -d '{"query":"secant formula","top_k":5}'
```

### 4. Include LLM answer generation

```bash
make generate
# or:
curl -X POST localhost:8080/search \
  -H "Content-Type: application/json" \
  -d '{"query":"secant formula","top_k":5,"generate":true}' --no-buffer
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

Then run `make dev` again (or `curl -X POST localhost:8080/ingest` on a running server). Only new/changed files are processed (SHA-256 dedup).

## Eval (retrieval quality)

The `cmd/eval` binary tests retrieval quality against labeled ground truth.

### Ground truth (golden sets)

Place YAML files in `golden/`. A template with field docs is at `golden/template.yaml`:

```yaml
# golden/my-dataset.yaml
queries:
  - query: "secant formula"
    expected_files:
      - "numerical-methods.md"
    relevance:
      "numerical-methods.md": 3
      "trig-functions.md": 1
    expected_answer: "The secant formula is ..."
```

File matching is by suffix — `"math/trig.md"` matches stored `"gitbook/math/trig.md"`.

### Run eval

```bash
make eval golden=golden/samples.yaml          # retrieval metrics
make eval-rag golden=golden/samples.yaml      # RAGAS metrics (needs LLM)
make eval-both golden=golden/samples.yaml     # both in one pass
```

Output is printed to stdout and saved to `results/` as timestamped JSON:

```
results/2026-06-28T13-32-16_retrieval.json
```

Each JSON file contains:

| Field | Description |
|-------|-------------|
| `meta` | Run metadata (timestamp, golden, mode, config) |
| `aggregate` | Aggregate metrics with confidence intervals |
| `queries` | Per-query breakdown with scores, latency, retrieved files |

### Per-query JSON fields

| Field | Description |
|-------|-------------|
| `query` | Search query text |
| `expected_files` | Ground-truth relevant files from golden set |
| `retrieved` | File paths retrieved (deduped, ranked) |
| `retrieved_files` | Files with raw similarity scores |
| `latency_ms` | Search latency in milliseconds |
| `recall_at_5`, `ndcg_at_10`, ... | Per-query metric values |

## Run separately

```bash
# 1. Start Docker services (Qdrant + reranker)
docker compose up -d qdrant reranker

# 2. Start Go server
make run

# 3. Ingest documents
make ingest
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
| `OLLAMA_ADDR` | `http://host.docker.internal:11434` | Ollama host |
| `RERANKER_ADDR` | `http://reranker:5002` | Reranker sidecar |
| `QDRANT_COLLECTION` | `documents_chunks` | Qdrant collection name |
| `LOGGER_LEVEL` | `prod` | `dev` or `prod` |

> `make dev` overrides these to `localhost:*` so Go server on host can reach Docker services.

## Routes

| Method | Path | Description |
|--------|------|-------------|
| POST | `/ingest` | Walk document dir, chunk+embed new/changed files |
| POST | `/search` | Hybrid semantic search over embedded chunks |

## Architecture

```
POST /ingest → ingest.Service (walk + SHA dedup) → Pipeline
                                          ├── Chunker (recursive / sentence-window)
                                          ├── Embedder (Ollama)
                                          └── Store.Upsert (Qdrant)

POST /search → Embedder → Store.HybridSearch (dense + sparse → RRF)
                                          └── [Reranker] → response
```

## Run tests

### Unit tests (no Docker required)

```bash
make test        # unit tests only; runs in seconds
make test-all    # all tests (requires Qdrant)
```

## PDF ingestion

Docling converts PDFs to markdown for ingestion. See `make docling`.

```bash
make docling-install   # one-time: install Python deps
make docling            # convert PDFs → markdown
make ingest             # ingest converted markdown
```

## Troubleshooting

### `make dev` fails with connection errors

Ensure Docker is running and no other services occupy ports 6333/6334/5002/8080. Run `make reset` to clear stale Qdrant state and retry.

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
lsof -i :8080
# Change http.addr in config/config.yaml if needed
```
