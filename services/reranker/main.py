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
    onnx       dynamic-int8 ONNX (default). The image bakes an int8 export of
               the build-time model (quantize.py); a runtime-swapped model
               falls back to fp32 ONNX unless the image is rebuilt.
    torch-int8 PyTorch-native dynamic int8, quantized at startup. No export
               step, works with any swappable model; boots slower and runs a
               bit slower than the baked ONNX export.
    openvino   fp32 OpenVINO (optional extra).
    torch      fp32 PyTorch — the escape hatch and last-resort fallback.

Install:
    pip install "sentence-transformers[onnx]" fastapi uvicorn

Run:
    RERANKER_MODEL=BAAI/bge-reranker-v2-m3 python services/reranker/main.py
    # or: uvicorn services.reranker.main:app --port 5002

API:
    POST /rerank  {"query": "...", "passages": ["...", ...]}
    -> {"scores": [0.95, -2.3, ...]}   # parallel to passages, higher = more relevant

    GET /health -> {"status": "ok"}
"""

import os
from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI
from pydantic import BaseModel
from sentence_transformers import CrossEncoder

MODEL_NAME = os.environ.get("RERANKER_MODEL", "BAAI/bge-reranker-v2-m3")
# Bound sequence length so CPU inference latency stays predictable regardless
# of what a swapped-in model advertises as its native context window.
MAX_LENGTH = int(os.environ.get("RERANKER_MAX_LENGTH", "512"))
BACKEND = os.environ.get("RERANKER_BACKEND", "onnx")
# Directory holding the build-time int8 export (see quantize.py).
QUANTIZED_DIR = os.environ.get("RERANKER_QUANTIZED_DIR", "int8_avx2")

_model: CrossEncoder | None = None


class TorchInt8Reranker:
    """Minimal pair scorer over a torch-native dynamic-int8 model.

    Quantizes Linear weights to int8 at startup (PyTorch native) — peaks at
    fp32 + int8 in memory (~2x the fp32 footprint during startup, then drops
    to roughly fp32/4 once the fp32 weights are freed), so it works on
    machines where the ONNX route's protobuf parse OOMs. Boot cost is the
    quantization pass itself (seconds for base-size models, a couple of
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
        self.tokenizer = AutoTokenizer.from_pretrained(model_name)
        model = AutoModelForSequenceClassification.from_pretrained(model_name)
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

    def to(self, device):
        return self  # CPU-only; CrossEncoder-interface compatibility


def baked_quantized_file() -> str | None:
    """Return the baked int8 onnx filename if it matches RERANKER_MODEL.

    The baked export is tied to the model it was built from (recorded in
    model_name.txt); loading it for a different model would silently score
    with the wrong weights.
    """
    onnx_dir = os.path.join(QUANTIZED_DIR, "onnx")
    try:
        with open(os.path.join(QUANTIZED_DIR, "model_name.txt")) as f:
            if f.read().strip() != MODEL_NAME:
                return None
        return next(
            name
            for name in os.listdir(onnx_dir)
            if name.startswith("model_") and name.endswith(".onnx")
        )
    except (OSError, StopIteration):
        return None


def load_model() -> CrossEncoder:
    """Load the configured backend, degrading to torch fp32 on failure.

    A quantized (or ONNX) load that dies at startup would take the whole
    sidecar — and with it retrieval quality — down with it, while a fp32
    fallback merely costs latency.
    """
    if BACKEND == "onnx":
        fname = baked_quantized_file()
        if fname is not None:
            print(f"loading baked int8 onnx model {fname!r} for {MODEL_NAME}")
            return CrossEncoder(
                QUANTIZED_DIR,
                backend="onnx",
                model_kwargs={"file_name": fname},
                max_length=MAX_LENGTH,
            )
        print(f"no baked int8 export matching {MODEL_NAME}; loading fp32 onnx")
    if BACKEND in ("onnx", "openvino"):
        try:
            return CrossEncoder(MODEL_NAME, backend=BACKEND, max_length=MAX_LENGTH)
        except Exception as e:
            print(f"backend {BACKEND!r} failed to load ({e!r}); falling back to torch fp32")
    if BACKEND == "torch-int8":
        return TorchInt8Reranker(MODEL_NAME, MAX_LENGTH)
    return CrossEncoder(MODEL_NAME, max_length=MAX_LENGTH)


@asynccontextmanager
async def lifespan(app: FastAPI):
    global _model
    _model = load_model()
    yield


app = FastAPI(lifespan=lifespan)


class RerankRequest(BaseModel):
    query: str
    passages: list[str]


class RerankResponse(BaseModel):
    scores: list[float]


@app.post("/rerank", response_model=RerankResponse)
def rerank(req: RerankRequest) -> RerankResponse:
    pairs = [[req.query, passage] for passage in req.passages]
    scores = _model.predict(pairs).tolist()
    return RerankResponse(scores=scores)


@app.get("/health")
def health():
    return {"status": "ok"}


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=5002)
