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
| `RERANKER_TRUST_REMOTE_CODE` | `false` | Explicitly allow a model's custom Transformers code |
| `RERANKER_PORT` | `5002` | HTTP listen port |
| `RERANKER_MAX_CONCURRENT` | `1` | Maximum simultaneous model calls |
| `RERANKER_MAX_LENGTH` | `512` | Maximum token length per query/passage pair |
| `RERANKER_QUANTIZED_DIR` | `int8_avx2` | Directory containing an optional baked ONNX model |

The model is downloaded at startup when running locally. The container image
pre-downloads the build-time model; changing `RERANKER_MODEL` at runtime may
require rebuilding the image to use a matching baked quantized model. The
build-time int8 bake (`BAKE_QUANTIZED=1`) exports the model to ONNX and
transiently needs more than 10 GB of host RAM for the current default model —
16 GB laptops with a desktop session should keep the Compose default
(`BAKE_QUANTIZED=0`, `RERANKER_BAKE_QUANTIZED=0`) and use the torch backend.

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
- The GPU Compose overlay is intended for Linux or Windows WSL2 with an
  NVIDIA CDI spec. It adds the CUDA build and passes the GPU through
  `nvidia.com/gpu=all`.
- macOS and CPU-only Windows hosts use the CPU image. Apple Silicon
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

## API performance and retrieval quality

Use the [Locust suite](../../benchmark/README.md) to measure retrieval and chat
through the Nadir API with this sidecar enabled. Direct sidecar and model-profile
benchmark tools were retired October 5. Locust records API workflow latency and
throughput alongside the API's available process metrics.

Run separately configured API instances with reranking enabled and disabled,
then use the same dataset with `make eval`. See the [Ragas evaluator guide](../../eval/README.md)
for capture, re-scoring and metric limits. Record the sidecar model, backend,
device and hardware; keep input questions and indexed source versions fixed.

The repository launcher `./scripts/local.sh` starts this sidecar from the
project virtual environment and starts Qdrant and the Go API separately.
