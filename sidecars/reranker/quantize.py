"""Build-time helper: bake a dynamic-int8 ONNX copy of the reranker model.

Run inside the Docker build (see Dockerfile). Produces ./int8_avx2/ holding
the quantized graph (onnx/model_quint8_avx2.onnx) plus the tokenizer/config
needed to load it standalone, and model_name.txt, which records the model it
was built from — main.py only uses the baked copy when RERANKER_MODEL still
matches, and degrades otherwise.

Dynamic (weight-only) int8 quantization needs no calibration data and keeps
retrieval quality within noise of fp32 while cutting CPU inference time by
roughly an order of magnitude on large cross-encoders. See
https://sbert.net/docs/cross_encoder/usage/efficiency.html.
"""

import os
import sys

from sentence_transformers import CrossEncoder
from sentence_transformers.backend import export_dynamic_quantized_onnx_model
from transformers import AutoConfig

MODEL_NAME = os.environ.get("RERANKER_MODEL", "BAAI/bge-reranker-v2-m3")
OUT_DIR = "int8_avx2"
# The exporter always lands the quantized graph here (hub-style layout).
BAKED_FILE = "onnx/model_quint8_avx2.onnx"
MARKER = os.path.join(OUT_DIR, "model_name.txt")


def main() -> None:
    if os.path.exists(MARKER):
        print(f"int8 model already baked in {OUT_DIR}/; skipping")
        return

    os.makedirs(OUT_DIR, exist_ok=True)
    model = CrossEncoder(MODEL_NAME, backend="onnx")
    export_dynamic_quantized_onnx_model(model, "avx2", OUT_DIR)
    if not os.path.exists(os.path.join(OUT_DIR, BAKED_FILE)):
        raise SystemExit(f"quantized export missing: {OUT_DIR}/{BAKED_FILE}")

    # The exporter writes only the graph; tokenizer and config come from the
    # base model so the directory loads without the HF cache.
    model.tokenizer.save_pretrained(OUT_DIR)
    AutoConfig.from_pretrained(MODEL_NAME).save_pretrained(OUT_DIR)
    with open(MARKER, "w") as f:
        f.write(MODEL_NAME)
    print(f"baked int8 onnx model for {MODEL_NAME} in {OUT_DIR}/")


if __name__ == "__main__":
    sys.exit(main())
