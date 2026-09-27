# nadir

Semantic document search engine. Ingests text files, chunks + embeds them locally, stores in Qdrant, serves hybrid semantic+keyword search over HTTP, with optional cross-encoder reranking and LLM answer generation.

## Prerequisites

| Tool | Required? | Purpose |
|------|-----------|---------|
| [Podman](https://podman.io) + podman-compose | **Required for the provided local/Compose flow** | Qdrant and optional containerized reranker (rootless; `./scripts/setup_podman_host.sh` sets the laptop up) |
| Go 1.27+ | **Required** | Server + CLI |
| Python 3.12+ | **Required for the host reranker/PDF sidecar** | Reranker sidecar and optional PDF conversion (the `numpy==2.5.2` pin needs 3.12) |
| Node.js 22+ | **Required for dashboard** | React dashboard and browser tests |
| [Ollama](https://ollama.com) | **Required** | Embeddings (`nomic-embed-text`) and optional LLM features |

```bash
ollama pull nomic-embed-text
ollama pull gemma3:1b   # for answer generation
```

## Quick start

### 1. Configure your data source

Edit `internal/bootstrap/configuration/config.yaml` → `documents.paths` to point at your source documents:

```yaml
documents:
  mode: "upload-only"     # upload-only | mirror
  paths:
    - "samples"           # ships with sample math docs
    - "~/my-documents"    # your own data
```

`upload-only` keeps existing Documents when a source file disappears. Set
`mode: mirror` when the configured directories are the complete corpus; a
successful source sweep then removes indexed files missing from those
directories. Multipart uploads never trigger mirror deletion.

### 2. Start everything

```bash
./scripts/local.sh
```

This starts Qdrant, the host-side reranker, and the Go API, ingests all source
files, and blocks on the server. Run the React dashboard separately:

```bash
cd web/dashboard
npm ci
npm run dev
```

Then open `http://localhost:3002`. Set `DASHBOARD_PORT` if that port is also
occupied; Vite uses a strict port and will fail clearly instead of silently
switching to a different URL.

### 3. Test search

```bash
curl -X POST localhost:8100/api/v1/turns \
  -H 'content-type: application/json' \
  -d '{"query":"secant formula","generate":false}'
```

The control API uses JSON requests and responses. Live generated answers are
delivered by an SSE endpoint in the turn response; the React dashboard handles
that stream.

### 4. Include LLM answer generation

Set `generate` to `true` to run answer generation over the retrieved chunks:

```bash
curl -X POST localhost:8100/api/v1/turns \
  -H 'content-type: application/json' \
  -d '{"query":"secant formula","generate":true}'
```

## Source data

The server reads Markdown and, when Docling is enabled, PDF source files from
directories listed in `config.yaml` → `documents.paths`. Each documents path is
walked recursively; files matching `documents.ignore_patterns` are skipped.
Source handling is controlled by `documents.mode`: `upload-only` retains indexed
files that are no longer present, while `mirror` removes missing files after a
fully successful sweep. A failed conversion or embedding pass never triggers
destructive reconciliation.

A sample set is included at `samples/` (4 math files). To use your own data:

```yaml
# internal/bootstrap/configuration/config.yaml
documents:
  paths:
    - "/path/to/your/docs"
    - "/another/directory"
```

Then run `./scripts/local.sh` again (or `curl -X POST localhost:8100/api/v1/documents` on a running server). Only new/changed files are processed (SHA-256 dedup).

## Run separately

```bash
# 1. Start Podman services (Qdrant + reranker)
podman compose -f deploy/compose/compose.yaml up -d qdrant reranker

# 2. Start Go server
go run ./cmd/api

# 3. Ingest documents
curl -X POST localhost:8100/api/v1/documents
```

## Podman Compose (Linux, Windows, macOS)

The default Compose stack is CPU-safe and does not require NVIDIA. It runs the
Go API, Qdrant, and the CPU reranker under rootless Podman on Linux, or inside
a `podman machine` VM on Windows and macOS; Ollama runs on the host and the
container reaches it through `host.containers.internal` (injected by Podman).

```bash
podman compose -f deploy/compose/compose.yaml up -d --build
```

One-time host setup — engine, rootless subordinate IDs, the newest
`podman-compose` provider, and the NVIDIA CDI toolchain on GPU machines:

```bash
./scripts/setup_podman_host.sh
```

The dashboard is not built or served by Compose. Start it with the local Node
toolchain in a second terminal:

```bash
cd web/dashboard
npm ci
npm run dev
```

Open `http://localhost:3002` after Vite starts. Set `DASHBOARD_PORT` to choose
another available port; Vite uses a strict port and will fail clearly if it is
occupied.

The default source mount is `./samples`. Set `DOCUMENTS_DIR` in `.env` to a
different host directory. On Apple Silicon, keep the default CPU reranker
backend (`RERANKER_BACKEND=torch`); the AVX2 quantized artifact is skipped for
portable builds. Ollama can still use Apple Metal acceleration on the host.

On Linux or Windows WSL2 with an NVIDIA GPU, generate the CDI spec once and
opt into the GPU override:

```bash
sudo nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml
podman compose -f deploy/compose/compose.yaml -f deploy/compose/compose.gpu.yaml up -d --build
```

The GPU override is optional. Do not use it on macOS.

## Config

Config file: `internal/bootstrap/configuration/config.yaml`. All keys with defaults are shown there — edit directly.

### Minimal config

```yaml
# internal/bootstrap/configuration/config.yaml
documents:
  paths:
    - "~/documents"
```

Disabled features and bounded operational knobs have sensible defaults. Enabled
external roles require their address/model in the config. For a full reference
of every knob, open `internal/bootstrap/configuration/config.yaml`.

### Env vars

| Var | Default (Podman Compose) | Purpose |
|-----|--------------------------|---------|
| `QDRANT_ADDR` | `qdrant:6334` | Qdrant gRPC address |
| `QDRANT_COLLECTION` | `documents_chunks` | Qdrant collection name |
| `OLLAMA_ADDR` | `http://host.containers.internal:11434` | Ollama host |
| `GENERATOR_ADDR` / `GENERATOR_MODEL` | same host / `gemma3:1b` | Explicit answer-generation endpoint and model |
| `GENERATOR_MAX_OUTPUT_TOKENS` | `512` | Maximum answer output tokens sent to Ollama as `num_predict` |
| `REWRITE_ADDR` / `REWRITE_MODEL` | same host / `gemma3:1b` | Explicit follow-up-rewriting endpoint and model |
| `HYPE_ADDR` / `HYPE_MODEL` | same host / `gemma3:1b` | Explicit HyPE enrichment endpoint and model |
| `CONTEXTUAL_ADDR` / `CONTEXTUAL_MODEL` | same host / `gemma3:1b` | Explicit contextual-enrichment endpoint and model |
| `EMBEDDER_API_KEY` | — | Embedder API key, if required |
| `RERANKER_ADDR` | `http://reranker:5002` | Reranker sidecar |
| `RERANKER_ENABLED` | — | `true`/`1` to force-enable the reranker |
| `RERANKER_ADAPTIVE_ENABLED` | `false` | Gate reranking on dense/lexical disagreement or a weak fused margin; keep off until release-gated quality evidence supports the tradeoff |
| `RERANKER_ADAPTIVE_MARGIN_THRESHOLD` | `0.01` | Relative fused top-result margin below which adaptive reranking is required |
| `LOGGER_LEVEL` | `prod` | `dev` or `prod` |
| `SEMANTIC_CACHE_THRESHOLD` | — | Cosine similarity threshold for a cache hit |
| `DOCUMENTS_PATHS` | — | Comma-separated documents paths; Compose normally sets this to `/app/source` |
| `DOCUMENTS_MODE` | `upload-only` | `upload-only` retains removed files; `mirror` reconciles configured source roots |
| `DOCUMENTS_DIR` | `./samples` | Host directory mounted into Compose as `/app/source` |
| `RERANKER_BACKEND` | `torch` in CPU Compose | `torch`, `torch-int8`, `onnx`, or `openvino` |
| `RERANKER_DEVICE` | `cpu` in CPU Compose | `cpu`, `auto`, or `cuda` |
| `RERANKER_MAX_CONCURRENT` | `1` | Maximum simultaneous reranker inferences |
| `RERANKER_QUEUE_TIMEOUT` | `30s` | Maximum time waiting for a reranker slot |
| `INFERENCE_PROFILE` | `local` | `local` requires an explicit reranker device; `custom` permits `auto` |
| `INFERENCE_OLLAMA_KEEP_ALIVE` | `5m` | Ollama model residency after a request is idle |
| `RERANKER_GPU` | `0` in CPU Compose | Set to `1` only with the GPU Compose override |
| `DOCLING_ENABLED` | `false` | Enable PDF document intake |
| `DOCLING_ADDR` | `http://host.containers.internal:5003` in Compose | Docling sidecar address |

Role-specific request timeouts are configured in `internal/bootstrap/configuration/config.yaml` under
`embedder`, `generator`, `rewriter`, `enrichment`, `reranker`, and `docling`.
LLM and embedding concurrency is owned by the Ollama scheduler
(`OLLAMA_NUM_PARALLEL`) rather than a client-side gate; process-local gates
remain for single-writer indexing and destructive operations
(`GATES_INDEXING_*`, `GATES_DESTRUCTIVE_*`).
When an LLM role is enabled, its address and model are required explicitly:
`generator`, `rewriter`, `enrichment.hype`, and `enrichment.contextual` do not
inherit another role's endpoint or model. Compose supplies explicit role
environment overrides even when roles share one Ollama server.

The shipped `inference.profile: local` serializes Ollama work across embedding,
rewriting, enrichment, and streaming generation, and runs one CPU reranker
operation at a time. This is a process-local safety profile for laptops, not a
distributed rate limiter. Set an explicit CUDA device and `torch` backend only
with the GPU Compose override after measuring GPU capacity.

> `./scripts/local.sh` runs the server against `internal/bootstrap/configuration/config.yaml`'s `localhost:*` addresses directly — no env overrides needed. Compose uses Podman-internal service names and a portable CPU reranker by default.

## Routes

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/documents` | Ingest multipart uploaded files or configured sources |
| POST | `/api/v1/documents/reset` | Publish an empty Qdrant collection generation |
| POST | `/api/v1/turns` | Start one JSON Retrieval/chat turn |
| GET | `/api/v1/turns/:id/events` | Stream answer events over SSE |
| POST | `/api/v1/turns/:id/cancel` | Cancel generation and keep the partial answer |
| GET | `/api/v1/sessions` | List recent chat sessions |
| GET | `/api/v1/sessions/:id` | Read one session and its turns |
| DELETE | `/api/v1/sessions/:id` | Delete one session and its turns |
| DELETE | `/api/v1/sessions` | Delete all sessions and turns |
| GET | `/api/v1/health` | API health check |

## Architecture

```
React dashboard → JSON/SSE API → domain services

POST /api/v1/documents → document intake (.md or optional .pdf→.md) → indexing pass
                                      ├── Chunker (recursive / sentence-window)
                                      ├── Embedder (Ollama)
                                      └── versioned Document replacement (Qdrant)

POST /api/v1/turns → chat.Service.StartTurn
                 ├── search.Service → Embedder → hybrid search (dense + sparse → RRF) → [Reranker]
                 ├── [Generator] supervised streaming answer over retrieved chunks
                 └── History persist at terminal state (mutation-owned and revision-checked)
```

The in-process event broker keeps a bounded, ordered replay window for one
server instance. If horizontal scaling is required, put the turn event log
behind a shared backend such as Redis Streams and route or broadcast SSE
subscribers through that shared log.

The Go API and React dashboard are separate artifacts. The dashboard is run
locally with Vite, which proxies `/api/` plus SSE traffic to the Go API. Podman
Compose runs the backend dependencies and does not build a frontend container.
For production, serve the dashboard's built `dist/` directory from an
independently managed static host and proxy the versioned API/SSE paths to the
Go API.

## Documentation map

- [Overview](docs/OVERVIEW.md) — what Nadir does and how users experience it.
- [Architecture decisions](docs/adr/index.md) — accepted ADRs, including the
  single-node design boundaries and the Podman runtime decision.
- [Active TODO](TODO.md) — open engineering work and evaluation priorities.

## Run tests

### Unit tests (no containers required)

```bash
make test                       # unit tests only; excludes local Python venv
make check                      # tests + vet + build
go test -count=1 ./cmd/... ./internal/... # all Go tests (Qdrant as available)
```

## Evaluate Retrieval quality

The evaluation command runs the golden query set against the configured Qdrant
collection, bypasses semantic cache, and reports HitRate, Recall, MRR, nDCG,
and latency percentiles. It uses the configured reranker by default:

```bash
go run ./cmd/evaluator --runs 3
go run ./cmd/evaluator --no-rerank --runs 3
go run ./cmd/evaluator --ensure-ingest --report test/evaluation/reports/local.json
```

The active golden set is a schema-v3 pack of 133 expert-authored synthetic
user-intent queries with direct, formula, procedure, comparison, multi-hop,
ambiguous, negative, and distractor cases. It records the sample corpus
manifest and two synthetic judgment passes, but contains no production user
data and is not a release gate. Historical reports still contain the original
34-query measurements. Collect consent-safe production queries, privacy
approval, and independent expert judgments before treating a score as a
production release gate.

A public ARQMath Task 1 candidate is also checked in under
[`test/evaluation/arqmath/`](test/evaluation/arqmath/). It contains 120
selected math questions—40 from each 2020–2022 edition—plus pinned source
metadata and fixed qrel candidate pools. It remains `release_gate: false`:
the full licensed corpus, two genuine independent reviewer passes,
adjudication, and privacy/legal approval must be supplied externally. Build
and review instructions are in its README.

## PDF ingestion

PDFs can be ingested directly when the Docling sidecar is enabled. The Go
indexing pass keeps the original PDF path as the source identity after
conversion.

With Podman Compose:

```bash
DOCLING_ENABLED=true HOST_DOCLING_ADDR=http://docling:5003 \
  podman compose -f deploy/compose/compose.yaml --profile pdf up -d --build
curl -X POST localhost:8100/api/v1/documents
```

For host-side development, start the sidecar and enable it in
`internal/bootstrap/configuration/config.yaml`:

```bash
pip install -r sidecars/document-converter/requirements.txt   # one-time: install Python deps
python sidecars/document-converter/main.py                    # HTTP sidecar on :5003
curl -X POST localhost:8100/api/v1/documents                 # ingests .md and .pdf sources
```

The directory CLI remains available when a separate offline conversion step
is preferred.

## Troubleshooting

### `./scripts/local.sh` fails with connection errors

Ensure the Podman Compose stack is up and no other services occupy ports 6333/6334/5002/8100. Clear stale Qdrant state and retry:

Use `POST /api/v1/documents/reset` to publish an empty collection generation safely:

```bash
curl -X POST localhost:8100/api/v1/documents/reset
```

### Ollama connection refused

```bash
curl http://localhost:11434/api/tags
ollama serve
```

### Ollama embedding fails or the GPU is out of memory

Check which models and processes are resident before changing the embedding
model:

```bash
ollama ps
nvidia-smi                 # NVIDIA hosts only
ollama stop <idle-model>   # unload an unused resident model
```

The host-side local script can place the reranker on the GPU. On a small GPU,
run the reranker in CPU mode or use the portable Compose profile so the
generator, embedder, and reranker do not compete for the same memory. Changing
an embedding model, vector dimension, or query/document prefix requires a full
document reindex.

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
# Change http.addr in internal/bootstrap/configuration/config.yaml if needed
```
