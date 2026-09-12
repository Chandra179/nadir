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

Edit `config/config.yaml` → `source.paths` to point at your source documents:

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
header for follow-up turns. Add `-F generate=on` to include an LLM answer.

### 4. Include LLM answer generation

Pass `generate=on` to run answer generation over the retrieved chunks:

```bash
curl -X POST localhost:8100/retrieval/search \
  --data-urlencode "query=secant formula" -F generate=on
```

## Source data

The server reads Markdown and, when Docling is enabled, PDF source files from
directories listed in `config.yaml` → `source.paths`. Each source path is
walked recursively; files matching `source.ignore_patterns` are skipped.

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

## Docker Desktop (Linux, Windows, and macOS)

The default Compose stack is CPU-safe and does not require NVIDIA. It works
with Docker Desktop on Windows and macOS; Ollama runs on the host and the
container reaches it through `host.docker.internal`.

```bash
docker compose up -d --build
curl -X POST localhost:8100/ingest
```

The default source mount is `./samples`. Set `SOURCE_DIR` in `.env` to a
different host directory. On Apple Silicon, keep the default CPU reranker
backend (`RERANKER_BACKEND=torch`); the AVX2 quantized artifact is skipped for
portable builds. Ollama can still use Apple Metal acceleration on the host.

On Linux or Windows with Docker Desktop + WSL2 and the NVIDIA Container
Toolkit, opt into the GPU override:

```bash
docker compose -f docker-compose.yml -f docker-compose.gpu.yml up -d --build
```

The GPU override is optional. Do not use it on macOS.

## Config

Config file: `config/config.yaml`. All keys with defaults are shown there — edit directly.

### Minimal config

```yaml
# config/config.yaml
source:
  paths:
    - "~/documents"
```

Disabled features and bounded operational knobs have sensible defaults. Enabled
external roles require their address/model in the config. For a full reference
of every knob, open `config/config.yaml`.

### Env vars

| Var | Default (docker-compose) | Purpose |
|-----|--------------------------|---------|
| `QDRANT_ADDR` | `qdrant:6334` | Qdrant gRPC address |
| `QDRANT_COLLECTION` | `documents_chunks` | Qdrant collection name |
| `OLLAMA_ADDR` | `http://host.docker.internal:11434` | Ollama host |
| `GENERATOR_ADDR` / `GENERATOR_MODEL` | same host / `gemma3:1b` | Explicit answer-generation endpoint and model |
| `REWRITE_ADDR` / `REWRITE_MODEL` | same host / `gemma3:1b` | Explicit follow-up-rewriting endpoint and model |
| `HYPE_ADDR` / `HYPE_MODEL` | same host / `gemma3:1b` | Explicit HyPE enrichment endpoint and model |
| `CONTEXTUAL_ADDR` / `CONTEXTUAL_MODEL` | same host / `gemma3:1b` | Explicit contextual-enrichment endpoint and model |
| `EMBEDDER_API_KEY` | — | Embedder API key, if required |
| `RERANKER_ADDR` | `http://reranker:5002` | Reranker sidecar |
| `RERANKER_ENABLED` | — | `true`/`1` to force-enable the reranker |
| `LOGGER_LEVEL` | `prod` | `dev` or `prod` |
| `SEMANTIC_CACHE_THRESHOLD` | — | Cosine similarity threshold for a cache hit |
| `SOURCE_PATHS` | — | Comma-separated source paths; Compose normally sets this to `/app/source` |
| `SOURCE_DIR` | `./samples` | Host directory mounted into Compose as `/app/source` |
| `RERANKER_BACKEND` | `torch` in CPU Compose | `torch`, `torch-int8`, `onnx`, or `openvino` |
| `RERANKER_DEVICE` | `cpu` in CPU Compose | `cpu`, `auto`, or `cuda` |
| `RERANKER_GPU` | `0` in CPU Compose | Set to `1` only with the GPU Compose override |
| `DOCLING_ENABLED` | `false` | Enable PDF document intake |
| `DOCLING_ADDR` | `http://host.docker.internal:5003` in Compose | Docling sidecar address |

Role-specific request timeouts are configured in `config/config.yaml` under
`embedder`, `generator`, `rewriter`, `enrichment`, `reranker`, and `docling`.
When an LLM role is enabled, its address and model are required explicitly:
`generator`, `rewriter`, `enrichment.hype`, and `enrichment.contextual` do not
inherit another role's endpoint or model. Compose supplies explicit role
environment overrides even when roles share one Ollama server.

> `./scripts/local.sh` runs the server against `config/config.yaml`'s `localhost:*` addresses directly — no env overrides needed. Compose uses Docker-internal service names and a portable CPU reranker by default.

## Routes

| Method | Path | Description |
|--------|------|-------------|
| POST | `/ingest` | Ingest uploaded files: chunk+embed new/changed ones (SHA-256 dedup) |
| POST | `/store/reset` | Drop and recreate the Qdrant collection |
| GET | `/retrieval` | Chat UI |
| POST | `/retrieval/search` | One chat turn: retrieve → (optional) generate → persist |
| GET | `/history/sessions` | Recent chat sessions (sidebar) |
| DELETE | `/history/sessions/:id` | Delete one persisted chat session and its turns |
| DELETE | `/history/sessions` | Delete all persisted chat sessions and turns |
| GET | `/history/sessions/:id` | Replay a past session |
| GET | `/healthz` | Health check |

## Architecture

```
POST /ingest → document intake (.md or optional .pdf→.md) → indexing pass
                                      ├── Chunker (recursive / sentence-window)
                                      ├── Embedder (Ollama)
                                      └── versioned Document replacement (Qdrant)

POST /retrieval/search → chat.Service.StartTurn
                 ├── search.Service → Embedder → hybrid search (dense + sparse → RRF) → [Reranker]
                 ├── [Generator] supervised streaming answer over retrieved chunks
                 └── History persist at terminal state (mutation-owned and revision-checked)
```

The in-process event broker keeps a bounded, ordered replay window for one
server instance. If horizontal scaling is required, put the turn event log
behind a shared backend such as Redis Streams and route or broadcast SSE
subscribers through that shared log.

## Run tests

### Unit tests (no Docker required)

```bash
make test                       # unit tests only; excludes local Python venv
make check                      # tests + vet + build
go test -count=1 ./config ./cmd/... ./internal/... # all Go tests (Qdrant as available)
```

## Evaluate Retrieval quality

The evaluation command runs the golden query set against the configured Qdrant
collection, bypasses semantic cache, and reports HitRate, Recall, MRR, nDCG,
and latency percentiles. It uses the configured reranker by default:

```bash
go run ./cmd/evalbench --runs 3
go run ./cmd/evalbench --no-rerank --runs 3
go run ./cmd/evalbench --ensure-ingest --report tests/eval/reports/local.json
```

The default golden set is intentionally small and is a Retrieval regression
fixture, not evidence that generated answers are faithful. Expand it with
real Documents and add generation-quality evaluation before treating a score
as a production release gate.

## PDF ingestion

PDFs can be ingested directly when the Docling sidecar is enabled. The Go
indexing pass keeps the original PDF path as the source identity after
conversion.

With Docker Compose:

```bash
DOCLING_ENABLED=true DOCKER_DOCLING_ADDR=http://docling:5003 \
  docker compose --profile pdf up -d --build
curl -X POST localhost:8100/ingest
```

For host-side development, start the sidecar and enable it in
`config/config.yaml`:

```bash
pip install -r services/docling/requirements.txt   # one-time: install Python deps
python services/docling/main.py                    # HTTP sidecar on :5003
curl -X POST localhost:8100/ingest                 # ingests .md and .pdf sources
```

The directory CLI remains available when a separate offline conversion step
is preferred.

## Troubleshooting

### `./scripts/local.sh` fails with connection errors

Ensure Docker is running and no other services occupy ports 6333/6334/5002/8100. Clear stale Qdrant state and retry:

```bash
curl -X DELETE localhost:6333/collections/documents_chunks
```

Use `POST /store/reset` to drop and recreate the Qdrant collection.

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
