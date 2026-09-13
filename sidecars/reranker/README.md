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
| `RERANKER_BACKEND` | `onnx` | `onnx`, `torch-int8`, `openvino`, or `torch` |
| `RERANKER_DEVICE` | `auto` | `auto`, `cpu`, or `cuda` |
| `RERANKER_MAX_LENGTH` | `512` | Maximum token length per query/passage pair |
| `RERANKER_QUANTIZED_DIR` | `int8_avx2` | Directory containing an optional baked ONNX model |

The model is downloaded at startup when running locally. The Docker image
pre-downloads the build-time model; changing `RERANKER_MODEL` at runtime may
require rebuilding the image to use a matching baked quantized model.

## CPU, GPU, and platform behavior

- The base Compose stack is CPU-safe and sets the portable `torch` backend.
- On CPU, the loader can use a baked int8 ONNX model, fp32 ONNX, torch-int8,
  or fp32 torch depending on the selected backend and available artifacts.
- With `RERANKER_DEVICE=auto` or `cuda`, a CUDA-capable host uses fp32 torch on
  the GPU. The int8 routes are CPU artifacts and are not used on CUDA.
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
RERANKER_DEVICE=auto RERANKER_BACKEND=torch \
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

The repository launcher `./scripts/local.sh` starts this sidecar from the
project virtual environment and starts Qdrant and the Go API separately.
