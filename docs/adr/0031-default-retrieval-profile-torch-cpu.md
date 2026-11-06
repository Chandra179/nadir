# 0031 — Default retrieval profile: BGE v2-M3 torch on CPU, chosen by pre-registered measurement

- **Status:** Accepted
- **Date:** 2026-09-27
- **Deciders:** Chandra

## Context

The retrieval default — BGE v2-M3 cross-encoder reranking enabled, torch fp32
on CPU — was an operational choice, never a measured release decision. The same
model could be served four ways: no reranker, torch fp32 on CPU, a dynamic-int8
AVX2 ONNX route on CPU, and torch fp32 on CUDA through the GPU Compose overlay.

Laptop evidence on the 133-query fixture covered three profiles: no reranker
(MRR@10 0.657), torch CPU (MRR@10 0.697, rerank p50 9.36s), and CUDA (MRR@10
0.697, rerank p50 435ms, peak VRAM ≈3.7 GiB). The int8 ONNX route had never been
measured end to end.

Acceptance was pre-registered: switch the CPU default to int8 ONNX only if
MRR@10 and HitRate@5 are each within 0.01 of torch and rerank p50 is at most
2 seconds. The full-corpus comparison (133 queries, 3 runs each) measured:

| Profile | HitRate@5 | MRR@10 | nDCG@5 | rerank p50/p95 |
|---|---|---|---|---|
| torch fp32 CPU | 0.812 | 0.697 | 0.709 | 9.36s / 18.6s |
| dynamic-int8 ONNX CPU | 0.820 | 0.725 | 0.731 | 5.42s / 9.70s |

Quality passed. Latency failed: p50 5.42s is above the 2s bar and only ≈1.7×
faster than torch.

## Decision

Keep the CPU default at torch fp32. The latency bar was not met, so the
sidecar `RERANKER_BACKEND` default stays `torch`. The GPU Compose overlay
remains the profile for latency-sensitive deployments (same quality at p50
435ms).

## Consequences

- The ONNX and OpenVINO backends, the build-time int8 bake and their
  dependencies are removed from the sidecar. Backends are now `torch` and
  `torch-int8`. A future ONNX revisit would need a new pre-registered
  measurement with fresh thresholds; these are spent.
- The evaluator report records the serving reranker profile
  (model/backend/device), so committed evidence is self-describing.
- The generation-judge gate stays open for a separate decision; this record
  settles only the retrieval profile.

## Current enablement (2026-10-03)

The `torch` CPU backend decision remains accepted for the optional sidecar.
[0034](0034-chunker-fixes-and-size.md) supersedes reranking enablement: the
current EmbeddingGemma workflow leaves reranking off because the measured
reranker regressed retrieval. This record does not recommend enabling it.
