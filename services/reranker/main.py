"""
Cross-encoder re-ranking sidecar.

Model is swappable via the RERANKER_MODEL env var (default: BAAI/bge-reranker-v2-m3,
~71.5 BEIR avg nDCG@10 vs ~60 for ms-marco-MiniLM-L-6-v2; MIT license).
Other known-good values:
    cross-encoder/ms-marco-MiniLM-L-6-v2   (tiny/fast baseline, ~60 BEIR)
    BAAI/bge-reranker-base                 (~67 BEIR, lighter than v2-m3)

Model paper: "Passage Re-ranking with BERT" (Nogueira & Cho 2019);
bge-reranker-v2-m3 is a multilingual XLM-RoBERTa-large based cross-encoder.

Install:
    pip install sentence-transformers fastapi uvicorn

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
_model: CrossEncoder | None = None


@asynccontextmanager
async def lifespan(app: FastAPI):
    global _model
    _model = CrossEncoder(MODEL_NAME, max_length=MAX_LENGTH)
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
