"""
SPLADE sparse embedding sidecar.
Uses fastembed SparseTextEmbedding (prithivida/Splade_PP_en_v1).

Install:
    pip install fastembed fastapi uvicorn

Run:
    python cmd/splade/main.py
    # or: uvicorn cmd.splade.main:app --port 5001

API:
    POST /embed_sparse  {"text": "...", "type": "query"|"passage"}
    -> {"indices": [...], "values": [...]}
"""

from contextlib import asynccontextmanager
from typing import Literal

import uvicorn
from fastembed import SparseTextEmbedding
from fastapi import FastAPI
from pydantic import BaseModel

MODEL_NAME = "prithivida/Splade_PP_en_v1"
_query_model: SparseTextEmbedding | None = None
_passage_model: SparseTextEmbedding | None = None


@asynccontextmanager
async def lifespan(app: FastAPI):
    global _query_model, _passage_model
    # fastembed downloads model on first use; same model object handles both modes
    _query_model = SparseTextEmbedding(model_name=MODEL_NAME)
    _passage_model = _query_model
    yield


app = FastAPI(lifespan=lifespan)


class EmbedRequest(BaseModel):
    text: str
    type: Literal["query", "passage"] = "passage"


class SparseVector(BaseModel):
    indices: list[int]
    values: list[float]


@app.post("/embed_sparse", response_model=SparseVector)
def embed_sparse(req: EmbedRequest) -> SparseVector:
    model = _query_model
    if req.type == "query":
        result = next(model.query_embed([req.text]))
    else:
        result = next(model.passage_embed([req.text]))
    return SparseVector(
        indices=result.indices.tolist(),
        values=result.values.tolist(),
    )


@app.get("/health")
def health():
    return {"status": "ok"}


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=5001)
