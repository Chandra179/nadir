"""
Cross-encoder re-ranking sidecar.

Model is swappable via the RERANKER_MODEL env var (default: BAAI/bge-reranker-v2-m3,
~71.5 BEIR avg nDCG@10 vs ~60 for ms-marco-MiniLM-L-6-v2; MIT license).
Other known-good values:
    cross-encoder/ms-marco-MiniLM-L-6-v2   (tiny/fast baseline, ~60 BEIR)
    BAAI/bge-reranker-base                 (~67 BEIR, lighter than v2-m3)

Model paper: "Passage Re-ranking with BERT" (Nogueira & Cho 2019);
bge-reranker-v2-m3 is a multilingual XLM-RoBERTa-large based cross-encoder.

Inference backend, via RERANKER_BACKEND:
    torch      fp32 PyTorch (default).
    torch-int8 PyTorch-native dynamic int8, quantized at startup. CPU only; no
               export step, works with any swappable model, boots slower.

Device, via RERANKER_DEVICE (auto | cpu | cuda, default cpu):
    "auto" picks cuda when torch sees a GPU. int8 is a CPU artifact, so on
    cuda RERANKER_BACKEND=torch is required. Explicit cuda fails readiness when
    CUDA is unavailable instead of silently falling back to CPU. The CUDA
    build of torch comes from the GPU image
    variant (deploy/compose/compose.gpu.yaml / Containerfile GPU=1 build arg).

Install:
    pip install sentence-transformers fastapi uvicorn

Run:
    RERANKER_MODEL=BAAI/bge-reranker-v2-m3 python sidecars/reranker/main.py
    # or: uvicorn main:app --app-dir sidecars/reranker --port 5002

API:
    POST /rerank  {"query": "...", "passages": ["...", ...]}
    -> {"scores": [0.95, -2.3, ...]}   # parallel to passages, higher = more relevant

    GET /health -> loaded model, backend, and device (503 with the load error
    while the runner is not ready)
"""

import os
import threading
from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI
from fastapi.responses import JSONResponse
from pydantic import BaseModel
from sentence_transformers import CrossEncoder

MODEL_NAME = os.environ.get("RERANKER_MODEL", "BAAI/bge-reranker-v2-m3")
# Bound sequence length so CPU inference latency stays predictable regardless
# of what a swapped-in model advertises as its native context window.
MAX_LENGTH = int(os.environ.get("RERANKER_MAX_LENGTH", "512"))
BACKEND = os.environ.get("RERANKER_BACKEND", "torch")
DEVICE_SETTING = os.environ.get("RERANKER_DEVICE", "cpu")
PORT = int(os.environ.get("RERANKER_PORT", "5002"))
TRUST_REMOTE_CODE = os.environ.get("RERANKER_TRUST_REMOTE_CODE", "false").lower() in {
    "1",
    "true",
    "yes",
}
MAX_CONCURRENT = max(1, int(os.environ.get("RERANKER_MAX_CONCURRENT", "1")))

_model: CrossEncoder | None = None
_load_error: str | None = None
_loaded_backend: str | None = None
_loaded_device: str | None = None
_inference_slots = threading.BoundedSemaphore(MAX_CONCURRENT)


def resolve_device() -> str:
    """Resolve RERANKER_DEVICE (auto|cpu|cuda) to a concrete torch device."""
    if DEVICE_SETTING == "cpu":
        return "cpu"
    if DEVICE_SETTING not in ("auto", "cuda"):
        raise ValueError(
            f"unknown RERANKER_DEVICE {DEVICE_SETTING!r}; expected cpu, cuda, or auto"
        )
    try:
        import torch

        if DEVICE_SETTING == "cuda":
            if not torch.cuda.is_available():
                raise RuntimeError(
                    "RERANKER_DEVICE=cuda but CUDA is unavailable; choose cpu explicitly"
                )
            return "cuda"
        if torch.cuda.is_available():
            return "cuda"
        print("no CUDA device available (CPU-only torch build or no GPU); auto selected cpu")
    except ImportError:
        if DEVICE_SETTING == "cuda":
            raise RuntimeError("RERANKER_DEVICE=cuda but torch is unavailable")
        print("torch unavailable; auto selected cpu")
    return "cpu"


class TorchInt8Reranker:
    """Minimal pair scorer over a torch-native dynamic-int8 model.

    Quantizes Linear weights to int8 at startup (PyTorch native) — peaks at
    fp32 + int8 in memory (~2x the fp32 footprint during startup, then drops
    to roughly fp32/4 once the fp32 weights are freed), so it works on
    constrained machines. Boot cost is the quantization pass itself (seconds for base-size models, a couple of
    minutes for v2-m3).

    Deliberately bypasses sentence-transformers' predict plumbing, which
    quantize_dynamic's deepcopy breaks, while matching CrossEncoder
    semantics for num_labels==1 models: sigmoid over the single logit,
    padded/truncated to MAX_LENGTH.
    """

    def __init__(self, model_name: str, max_length: int):
        import torch
        from torch.ao.quantization import quantize_dynamic
        from transformers import AutoModelForSequenceClassification, AutoTokenizer

        self.torch = torch
        self.tokenizer = AutoTokenizer.from_pretrained(
            model_name, trust_remote_code=TRUST_REMOTE_CODE
        )
        model = AutoModelForSequenceClassification.from_pretrained(
            model_name, trust_remote_code=TRUST_REMOTE_CODE
        )
        model.eval()
        self.model = quantize_dynamic(model, {torch.nn.Linear}, dtype=torch.qint8)
        self.max_length = max_length
        print(f"torch-int8 backend ready for {model_name}")

    def predict(self, pairs):
        torch = self.torch
        inputs = self.tokenizer(
            [list(pair) for pair in pairs],
            padding=True,
            truncation=True,
            max_length=self.max_length,
            return_tensors="pt",
        )
        with torch.inference_mode():
            logits = self.model(**inputs).logits
        scores = logits.squeeze(-1) if logits.ndim > 1 else logits
        return torch.sigmoid(scores)


def load_model() -> CrossEncoder:
    """Load the configured backend (torch fp32 or torch-int8)."""
    global _loaded_backend, _loaded_device
    device = resolve_device()
    _loaded_device = device
    if device == "cuda":
        if BACKEND != "torch":
            raise ValueError(
                f"RERANKER_DEVICE=cuda requires RERANKER_BACKEND=torch, got {BACKEND!r}"
            )
        _loaded_backend = "torch"
        return CrossEncoder(
            MODEL_NAME,
            max_length=MAX_LENGTH,
            device="cuda",
            trust_remote_code=TRUST_REMOTE_CODE,
        )

    if BACKEND == "torch-int8":
        _loaded_backend = "torch-int8"
        return TorchInt8Reranker(MODEL_NAME, MAX_LENGTH)
    _loaded_backend = "torch"
    return CrossEncoder(
        MODEL_NAME,
        max_length=MAX_LENGTH,
        device="cpu",
        trust_remote_code=TRUST_REMOTE_CODE,
    )


@asynccontextmanager
async def lifespan(app: FastAPI):
    global _model, _load_error
    try:
        _model = load_model()
        _load_error = None
    except Exception as exc:
        # Keep the process alive so orchestration can observe the failure and
        # restart it with useful diagnostics instead of seeing a connection
        # refusal with no runner context.
        _model = None
        _load_error = repr(exc)
        print(f"reranker model failed to load: {_load_error}")
    yield
    _model = None


app = FastAPI(lifespan=lifespan)


class RerankRequest(BaseModel):
    query: str
    passages: list[str]


class RerankResponse(BaseModel):
    scores: list[float]


@app.post("/rerank", response_model=RerankResponse)
def rerank(req: RerankRequest) -> RerankResponse:
    if _model is None:
        return JSONResponse(
            status_code=503,
            content={"detail": "reranker model is not ready", "error": _load_error},
        )
    if not _inference_slots.acquire(blocking=False):
        return JSONResponse(
            status_code=429,
            content={
                "detail": "reranker inference capacity is busy",
                "max_concurrent": MAX_CONCURRENT,
            },
        )
    try:
        pairs = [[req.query, passage] for passage in req.passages]
        scores = _model.predict(pairs).tolist()
        return RerankResponse(scores=scores)
    finally:
        _inference_slots.release()


@app.get("/health")
def health():
    if _model is None:
        return JSONResponse(
            status_code=503,
            content={
                "status": "not_ready",
                "model": MODEL_NAME,
                "backend": _loaded_backend or BACKEND,
                "device": _loaded_device or DEVICE_SETTING,
                "error": _load_error or "model is not loaded",
            },
        )
    return {
        "status": "ok",
        "model": MODEL_NAME,
        "loaded_model": MODEL_NAME,
        "backend": _loaded_backend or BACKEND,
        "device": _loaded_device or DEVICE_SETTING,
    }


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=PORT)
