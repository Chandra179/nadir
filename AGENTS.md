# AGENTS.md

## Commands

The Makefile provides `run`, `compose`, `test`, `race`, `vet`, `build`, and
`check` targets. It scopes Go checks to `./cmd/...` and `./internal/...`
so a local Python `venv/` is not discovered as a Go package.

```bash
# Build
go build ./cmd/api

# Vendor deps (NOT committed — gitignored; run after adding imports)
go mod tidy && go mod vendor

# Dev: Qdrant (Podman) + optional reranker (repo venv) + server + auto-ingest
./scripts/local.sh               # addrs come from internal/bootstrap/configuration/config.yaml (localhost)

# Run standalone (internal/bootstrap/configuration/config.yaml, .env sourced)
go run ./cmd/api

# Tests
go test -short -count=1 ./cmd/... ./internal/... # unit tests only
go test -count=1 ./cmd/... ./internal/...       # all Go tests
go test -run TestMatchPattern ./internal/core/documents/indexing/   # focused pkg test
make benchmark ARGS="--host http://127.0.0.1:8100 --query 'Explain the indexed document'" # Locust API load evidence

# Quick ops (server must be on :8100)
curl -X POST localhost:8100/api/v1/documents
curl -X POST localhost:8100/api/v1/turns -H 'content-type: application/json' -d '{"query":"What are the main ideas in the uploaded document?","generate":false}'
curl -X POST localhost:8100/api/v1/documents/reset                    # safely reset the Document collection

# Retrieval quality evaluation (Qdrant + embedder, optional reranker must be running)
DOCUMENTS_PATHS=/absolute/path/to/evaluation-documents QDRANT_COLLECTION=documents_chunks_evaluation go run ./cmd/evaluator --golden /absolute/path/to/questions.json --runs 3
DOCUMENTS_PATHS=/absolute/path/to/evaluation-documents QDRANT_COLLECTION=documents_chunks_evaluation go run ./cmd/evaluator --golden /absolute/path/to/questions.json --no-rerank --runs 3
```

## Architecture

`cmd/api` and `cmd/evaluator` are the normal entrypoints. `internal/bootstrap`
loads configuration and wires the process. `internal/edge/http` maps the
versioned JSON/SSE API with `net/http`. The evaluator is a separate CLI that
uses the same Retrieval and prompt code.

```
GET  /api/v1/documents → active file/version inventory and last import this process
POST /api/v1/documents → edge → Documents (intake → chunk → enrich → embed → versioned replace)
POST /api/v1/turns → edge → Conversation (session/edit → Retrieval → supervised generation)
GET  /api/v1/turns/{id}/events → SSE replay, including Last-Event-ID
POST /api/v1/turns/{id}/cancel → cancel generation and persist a partial answer
GET  /api/v1/health → liveness; GET /api/v1/ready → dependency readiness
```

- `internal/core/documents/` owns intake, chunking, optional enrichment,
  embeddings, versioned replacement, source mirroring, and reset.
- `internal/core/retrieval/` owns hybrid search, fusion, reranking, and cache.
- `internal/core/conversation/` owns sessions, edits, generation, event replay,
  cancellation, and history.
- `internal/core/observability/` owns provider-neutral operation correlation and
  bounded metrics. `internal/bootstrap/server` maps those metrics to HTTP.
- `internal/providers/` implements Ollama, Qdrant, reranker, and Docling seams.
- `internal/bootstrap/gates/` owns process-local operation gates for indexing
  and destructive operations plus bounded background jobs; LLM/embedding
  concurrency is owned by the Ollama scheduler.
- `internal/eval/` is the evaluator library; it shares retrieval and prompt
  building with the API.

The React dashboard remains in `web/dashboard`, and the Python reranker and
optional Docling processes remain in `sidecars/`. Compose builds `cmd/api`, so
normal local and Compose startup use the same backend.

## Key rules

- Core use cases must not import `internal/edge/http/`, `internal/providers/`,
  `internal/bootstrap/server/`, or frontend code.
- Retry logic lives in Documents indexing, never in an Embedder or Store.
- Chunk IDs = UUIDv5 over `filePath:sourceSHA:lineStart:chunkIndex` — versioned deterministic replacement; old versions are deactivated and cleaned after the new version is active
- Config: `internal/bootstrap/configuration/config.yaml` → `internal/bootstrap/configuration/config.go` `applyEnv()` overrides. Known env vars include `QDRANT_ADDR`, `QDRANT_COLLECTION`, `OLLAMA_ADDR`, `EMBEDDER_MODEL`, `EMBEDDER_DIMENSIONS`, `EMBEDDER_NUM_GPU`, `EMBEDDER_QUERY_PREFIX`, `EMBEDDER_DOCUMENT_PREFIX`, `EMBEDDER_MAX_INPUT_CHARS`, `GENERATOR_ADDR`, `GENERATOR_MODEL`, `GENERATOR_MAX_OUTPUT_TOKENS`, `GENERATOR_NUM_CTX`, `EMBEDDER_API_KEY`, `DOCUMENTS_PATHS`, `DOCUMENTS_MODE`, `DOCUMENTS_IGNORE_PATTERNS`, `FUSION_ENABLED`, `FUSION_RRF_K`, `FUSION_DENSE_WEIGHT`, `FUSION_BM25_WEIGHT`, `FUSION_EXACT_MATCH_BOOST`, `FUSION_HEADER_MATCH_BOOST`, `FUSION_MIN_EXACT_TOKENS`, `FUSION_MIN_HEADER_TOKENS`, `RERANKER_ADDR`, `RERANKER_ENABLED`, `RERANKER_MODEL`, `RERANKER_ADAPTIVE_ENABLED`, `RERANKER_ADAPTIVE_MARGIN_THRESHOLD`, `RERANKER_DEVICE`, `RERANKER_BACKEND`, `RERANKER_TRUST_REMOTE_CODE`, `RERANKER_PORT`, `RERANKER_MAX_CONCURRENT`, `RERANKER_QUEUE_TIMEOUT`, `INFERENCE_PROFILE`, `INFERENCE_OLLAMA_KEEP_ALIVE`, `GATES_<INDEXING|DESTRUCTIVE>_MAX_CONCURRENT`, `GATES_<INDEXING|DESTRUCTIVE>_QUEUE_TIMEOUT`, `HISTORY_SESSION_PAGE_SIZE`, `HISTORY_TURN_PAGE_SIZE`, `PROFILING_ENABLED`, `PROFILING_ADDR`, `LOGGER_LEVEL`, `SEMANTIC_CACHE_THRESHOLD`, `CONTEXTUAL_ENABLED`, `CONTEXTUAL_ADDR`, `CONTEXTUAL_MODEL`, `REWRITE_ENABLED`, `REWRITE_ADDR`, `REWRITE_MODEL`, `REWRITE_TURNS`, `HISTORY_ENABLED`, `HISTORY_COLLECTION`, `DOCLING_ENABLED`, `DOCLING_ADDR`
- Document source dirs are configured by `documents.paths`; `DOCUMENTS_PATHS` is a comma-separated override used by Compose and container deployments
- External Ollama/sidecar request timeouts are configured per role in `internal/bootstrap/configuration/config.yaml`; constructors retain defaults only for direct package tests. Enabled LLM roles must declare their own `ollama_addr` and `model`; they do not inherit another role's endpoint.
- Optional `embedder.num_gpu` (`EMBEDDER_NUM_GPU`) preserves Ollama placement when omitted; `0` runs embeddings on CPU, `-1` lets Ollama choose, and positive values request GPU layers. Measure model coexistence and latency before changing a hardware profile.
- Embedder task prefixes (`embedder.query_prefix`/`document_prefix`) apply at call sites, not in the embedder; changing either requires a reindex
- The enrichment flag (`enrichment.contextual.enabled`) affects ingest only; enabling after a prior ingest requires a reindex
- Retrieval fusion is opt-in under `search.fusion`; it uses weighted rank-RRF plus optional exact/header boosts and must be compared against the default Qdrant RRF path on the golden set before enabling.

## Addresses: local vs Podman Compose

`./scripts/local.sh` runs the host-side server against `internal/bootstrap/configuration/config.yaml`'s localhost addresses directly and prints the local dashboard command. The base Compose stack runs the backend services and is CPU-safe under rootless Podman on Linux and inside `podman machine` on macOS/Windows; layer `deploy/compose/compose.gpu.yaml` only on Linux or Windows WSL2 with an NVIDIA CDI spec (`sudo nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml`, or `./scripts/setup_podman_host.sh`). The dashboard is not a Compose service.

The API exposes bounded process-local operation metrics at `/debug/metrics`.
Request/trace IDs and domain child operation IDs are included in structured
logs; this endpoint is diagnostic telemetry, not a distributed metrics store.

## Features gated by config

| Feature | Config key | Requires |
|---------|-----------|----------|
| Answer generation | `generator.enabled` (on by default) | Ollama LLM; dashboard or `POST /api/v1/turns` with `generate=true` |
| Semantic cache | `semantic_cache.enabled` (on by default) | None (reuses Qdrant) |
| Reranker | `reranker.enabled` (off by default) | Reranker sidecar |
| Query rewriting | `rewriter.enabled` (on by default) | Ollama LLM; follow-up turns only (+1 LLM call); chat history enabled |
| Contextual retrieval | `enrichment.contextual.enabled` (off by default) | Ollama LLM; reindex after enabling |
| PDF document intake | `docling.enabled` (off by default) | Docling sidecar; source PDFs are converted before indexing |

Every enabled LLM role must declare its own `ollama_addr` and `model`; generator, rewriter, and contextual enrichment do not inherit another role's endpoint or model. The default `inference.profile: local` delegates LLM/embedding concurrency to the Ollama scheduler (`OLLAMA_NUM_PARALLEL`) with per-role request timeouts and a finite `keep_alive`, and limits the reranker to one explicit CPU operation behind a client-side queue. Set `RERANKER_DEVICE=cuda` and `RERANKER_BACKEND=torch` only with the GPU Compose override and a measured hardware budget; `auto` is reserved for `inference.profile: custom`. The base Compose stack is CPU-safe (`RERANKER_GPU=0`, `RERANKER_DEVICE=cpu`); `deploy/compose/compose.gpu.yaml` adds the CUDA build and the NVIDIA CDI device (`nvidia.com/gpu=all`) for Linux/Windows WSL2. The dev flow (`local.sh`) runs the sidecar from the repo `venv/` on the host with the explicit local CPU profile and only starts Qdrant via Podman. Apple Silicon should use the CPU `torch` backend; the AVX2 quantized bake is skipped for portable builds.
The `gates` section configures `internal/bootstrap/gates` process-wide finite queues for indexing (single-writer) and destructive operations. These budgets coordinate one API process only; they do not make Chat, Indexing, or model serving distributed-safe.

## Document and evaluation inputs

`documents.paths` defaults to an empty list. `./scripts/local.sh` skips automatic
ingestion until source directories are configured; upload chosen documents in
the dashboard paperclip instead. The base Compose stack also has no source mount; use
`deploy/compose/compose.sources.yaml` with an explicit `DOCUMENTS_DIR` for one.
The sample corpus has been removed. Historical evaluation fixtures remain for
schema checks and interpreting dated reports. Live evaluations require an
explicit query set and matching source directories in a separate collection,
as shown above. API performance workloads live in `benchmark/`; see its README
for the dedicated Python environment and explicit workload inputs. `scripts/`
keeps local startup, Podman setup and generic judge calibration utilities.

Evaluator and Locust outputs share the versioned run-report contract in
`test/run-report-contract.json`. Generated reports stay in ignored `.local/`
by default. The active result catalog retains four historical evaluator
measurements; manual app/workflow records and retired runner output are archived.
