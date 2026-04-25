"""
Cross-encoder re-ranking sidecar.
Uses cross-encoder/ms-marco-MiniLM-L-6-v2 from sentence-transformers.

Model paper: "Passage Re-ranking with BERT" (Nogueira & Cho 2019)
Trained on MS-MARCO; optimized for passage relevance scoring.
L-6 variant: fast (~6ms/pair CPU), 22M params.

Install:
    pip install sentence-transformers fastapi uvicorn

Run:
    python cmd/reranker/main.py
    # or: uvicorn cmd.reranker.main:app --port 5002

API:
    POST /rerank  {"query": "...", "passages": ["...", ...]}
    -> {"scores": [0.95, -2.3, ...]}   # parallel to passages, higher = more relevant

    GET /health -> {"status": "ok"}
"""

from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI
from pydantic import BaseModel
from sentence_transformers import CrossEncoder

MODEL_NAME = "cross-encoder/ms-marco-MiniLM-L-6-v2"
_model: CrossEncoder | None = None


@asynccontextmanager
async def lifespan(app: FastAPI):
    global _model
    _model = CrossEncoder(MODEL_NAME)
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
