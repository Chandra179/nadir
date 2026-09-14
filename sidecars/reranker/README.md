# Reranker sidecar

The reranker is a separate HTTP process that scores the passages returned by
retrieval. Nadir uses the scores to order the final candidate set before answer
generation. The implementation is a swappable sentence-transformers
cross-encoder.

## Interface

### `POST /rerank`

Request:

```json
{
  "query": "What is the secant formula?",
  "passages": ["...", "..."]
}
```

Response:

```json
{
  "scores": [0.95, 0.12]
}
```

Scores have the same order as `passages`; a higher score means that the
passage is more relevant to the query. If the model is not ready, the endpoint
returns HTTP `503`.

### `GET /health`

Returns HTTP `200` after the model is loaded:

```json
{
  "status": "ok",
  "model": "BAAI/bge-reranker-v2-m3",
  "loaded_model": "BAAI/bge-reranker-v2-m3",
  "backend": "torch",
  "device": "cpu"
}
```

During a failed or incomplete model load it returns HTTP `503` with
`status: "not_ready"` and the load error. This endpoint is suitable for the
Compose readiness check.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `RERANKER_MODEL` | `BAAI/bge-reranker-v2-m3` | Hugging Face model to load |
| `RERANKER_BACKEND` | `torch` | `onnx`, `torch-int8`, `openvino`, or `torch` |
| `RERANKER_DEVICE` | `cpu` | `auto`, `cpu`, or `cuda` |
| `RERANKER_MAX_CONCURRENT` | `1` | Maximum simultaneous model calls |
| `RERANKER_MAX_LENGTH` | `512` | Maximum token length per query/passage pair |
| `RERANKER_QUANTIZED_DIR` | `int8_avx2` | Directory containing an optional baked ONNX model |

The model is downloaded at startup when running locally. The Docker image
pre-downloads the build-time model; changing `RERANKER_MODEL` at runtime may
require rebuilding the image to use a matching baked quantized model.

## CPU, GPU, and platform behavior

- The base Compose stack is CPU-safe and sets the portable `torch` backend.
- On CPU, the loader can use a baked int8 ONNX model, fp32 ONNX, torch-int8,
  or fp32 torch depending on the selected backend and available artifacts.
- With `RERANKER_DEVICE=cuda`, a CUDA-capable host uses fp32 torch on
  the GPU. The int8 routes are CPU artifacts and are not used on CUDA.
- Explicit `cuda` fails readiness when CUDA is unavailable instead of silently
  falling back to CPU. `auto` is available only for custom deployments.
- The local profile uses one CPU reranker call at a time. Requests above that
  limit receive HTTP `429` instead of accumulating unbounded inference work.
- The GPU Compose overlay is intended for Linux or Windows WSL2 with the
  NVIDIA Container Toolkit. It adds the NVIDIA device reservation and CUDA
  dependencies.
- macOS and CPU-only Windows Docker Desktop use the CPU image. Apple Silicon
  can still use host-side Ollama acceleration, but this sidecar currently runs
  on CPU.

## Local run

From the repository root, install the dependencies in the project virtual
environment and start the process:

```bash
venv/bin/pip install -r sidecars/reranker/requirements.txt \
  -r sidecars/reranker/requirements-cpu.txt
RERANKER_DEVICE=cpu RERANKER_BACKEND=torch RERANKER_MAX_CONCURRENT=1 \
  venv/bin/python sidecars/reranker/main.py
```

Alternatively, start FastAPI with Uvicorn:

```bash
venv/bin/uvicorn main:app --app-dir sidecars/reranker --host 0.0.0.0 --port 5002
```

Check readiness with:

```bash
curl http://localhost:5002/health
```

## Benchmarking model and backend profiles

`scripts/benchmark_reranker.py` measures the running sidecar with a
consent-safe JSON corpus. Each query supplies candidate passages and an expert
relevance grade (`0` means non-relevant; positive grades are relevant). The
report contains HitRate@k, Recall@k, MRR@10, graded nDCG@k, p50/p95 latency,
sequential throughput, per-request failures/timeouts, and optional RSS/VRAM
samples. The health response records the exact loaded model, backend, and
device so reports from different profiles are comparable.

Example dataset:

```json
{
  "schema_version": 1,
  "metadata": {"provenance": "consent-safe expert judgments"},
  "queries": [
    {
      "id": "q-001",
      "query": "what is the secant formula?",
      "candidates": [
        {"text": "The secant formula is ...", "relevance": 2},
        {"text": "An unrelated passage ...", "relevance": 0}
      ]
    }
  ]
}
```

Run one profile from the repository root:

```bash
python scripts/benchmark_reranker.py \
  --dataset ./reranker-benchmark.json \
  --endpoint http://127.0.0.1:5002/rerank \
  --pid <sidecar-pid> \
  --runs 3 \
  --json-out test/evaluation/reports/reranker-bge-cpu.json
```

The committed Retrieval fixture can be benchmarked directly without creating a
second copy of its annotations. The adapter resolves each `relevant` and
`distractors` entry from `test/evaluation/golden.json` into passages from the
sample corpus:

```bash
python scripts/benchmark_reranker.py \
  --dataset test/evaluation/golden.json \
  --corpus-dir samples \
  --endpoint http://127.0.0.1:5002/rerank \
  --pid <sidecar-pid> \
  --runs 1 \
  --json-out test/evaluation/reports/reranker-bge-m3-golden.json
```

This is a repeatable synthetic-corpus baseline, not a production release
gate. Use the same golden fixture and corpus for every profile so quality and
resource measurements remain comparable.

For a production comparison, require the release-gate metadata and full
judgment set. This rejects the committed synthetic fixture and refuses to
truncate the dataset:

```bash
python scripts/benchmark_reranker.py \
  --dataset /path/to/production-golden.json \
  --corpus-dir /path/to/production-corpus \
  --require-release-gate \
  --endpoint http://127.0.0.1:5002/rerank \
  --runs 3 \
  --json-out test/evaluation/reports/reranker-production-bge.json
```

For Compose, replace `--pid` with `--container "$(docker compose -f
deploy/compose/docker-compose.yml ps -q reranker)"`. For a CUDA process, add
`--gpu-pid` to sample process VRAM through `nvidia-smi`. Repeat the command
after changing `RERANKER_MODEL`, `RERANKER_BACKEND`, or
`RERANKER_DEVICE`; do not compare reports unless their corpus, candidate
lists, runs, and hardware are the same. The health probe measures readiness,
not process startup; measure container startup separately when completing the
production comparison.

The direct benchmark isolates reranker behavior. Run the full evaluator as
well to measure end-to-end Retrieval quality and dependency latency:

```bash
go run ./cmd/evaluator --runs 3
go run ./cmd/evaluator --no-rerank --runs 3
```

The benchmark harness and unit tests use only the Python standard library;
they do not download models or create synthetic production evidence.

The repository launcher `./scripts/local.sh` starts this sidecar from the
project virtual environment and starts Qdrant and the Go API separately.
