# 0003 — Reranker sidecar int8 quantization: baked ONNX by default, torch-int8 as the no-bake route

- **Status:** Accepted
- **Date:** 2026-09-05
- **Deciders:** Chandra, ZCode session

## Context

The reranker sidecar ran `sentence_transformers.CrossEncoder` fp32 on CPU:
p50 ≈ 3.2–4.4s for one 20-pair batch (topK 5 × candidate_mul 4), well over
the 1–2s budget, while owning the latency budget of every search. The
documented paths (sbert's CrossEncoder efficiency benchmarks) are ONNX
dynamic int8 or OpenVINO qint8, both promising large CPU speedups with
NanoBEIR nDCG@10 tracking fp32 within noise.

Practical constraints discovered during implementation:

- The ONNX **bake** is memory-hungry: fp32 export peaks ~8–10GB; even
  quantizing from a saved graph peaks ~7GB (onnx protobuf parse). The dev
  desktop (15GB, ~10GB used, 4GB swap full) OOM-killed every v2-m3 attempt.
- OpenVINO qint8 requires **more** memory, not less (static quantization
  with a calibration dataset download) — rejected.
- fp16 export has the same export peak and is slow on CPU without fp16
  acceleration — rejected.
- Quantizing in PyTorch and then exporting produces a different graph than
  ONNX Runtime's MatMulInteger path — rejected as fiddly.
- No credible pre-quantized int8 ONNX of bge-reranker-v2-m3 exists on the
  Hub (community mirrors are fp32-only).
- The sidecar's model is swappable at runtime via `RERANKER_MODEL`; a baked
  artifact is valid only for the model it was built from.

## Decision

`RERANKER_BACKEND` selects the route (default `onnx`):

- **`onnx`** — `quantize.py` bakes a dynamic-int8 (avx2) ONNX export into
  the image at Docker build (`int8_avx2/`: tokenizer + config +
  `onnx/model_quint8_avx2.onnx` + `model_name.txt`). The sidecar loads the
  baked export only when `model_name.txt` matches `RERANKER_MODEL`; a
  runtime-swapped model **degrades to fp32 ONNX rather than loading the
  wrong weights**. `requirements.txt` moved to sentence-transformers 6.0.1
  (first-party `backend=` support + `export_dynamic_quantized_onnx_model`),
  torch 2.14+cpu, onnxruntime 1.29 — pinned from a validated set.
- **`torch-int8`** — PyTorch-native dynamic int8 (`quantize_dynamic`,
  Linear weights → qint8) applied at startup. No export step, no bake, works
  with any swappable model on any machine; costs boot time and is slower
  than the baked ONNX route (see ADR 0004 for why it needs its own scorer).
- **`torch`** — fp32, the escape hatch.

Every quantized/onnx load failure degrades to fp32 torch with a loud log: a
quantized load that dies at startup would take retrieval quality down with
it, while a fp32 fallback merely costs latency.

Measured evidence:

- ONNX int8 (ms-marco-MiniLM, through the production `load_model()`): p50
  365ms → 178ms (2.06×), top-1 agreement 5/5; weights ~2.3GB → ~0.6GB for
  bge-v2-m3 class models.
- torch-int8 (bge-v2-m3, evalbench A/B, 34 queries × 3 runs, reports
  `rerank_ab_*`): MRR@10 0.793 → 0.765, nDCG@5 0.826 → 0.798, p50 4358ms →
  3372ms (1.29×), ~1.5GB less RAM — keeps ~60% of the reranker's MRR gain
  over no-rerank (0.721).

## Consequences

- The default deployment (`docker compose build reranker`) serves int8 from
  the baked export with fp32-equal ranking expected (≈2× speedup per sbert's
  benchmarks) — pending the evalbench A/B on the baked artifact.
- The bake must run on a machine with ~8–10GB free (or from the saved fp32
  graph in `/tmp` on this desktop once memory frees); it is a one-time,
  portable artifact.
- torch-int8 is the zero-hassle route on constrained machines: quality dip
  (−2.8pp MRR vs fp32 on 34 queries) traded for no bake and ~1.5GB less RAM.
- Image size stays flat: the baked dir carries the int8 graph only; the fp32
  export it was derived from is dropped.
