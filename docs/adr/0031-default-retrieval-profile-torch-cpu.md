# 0031 — Default retrieval profile: BGE v2-M3 torch on CPU, chosen by pre-registered measurement

- **Status:** Accepted
- **Date:** 2026-09-27
- **Deciders:** Chandra

## Context

The retrieval default — BGE v2-M3 cross-encoder reranking enabled, torch fp32
on CPU — was an operational choice, never a measured release decision. The
same model can be served four ways: no reranker, torch fp32 on CPU, the
dynamic-int8 AVX2 ONNX route on CPU (sidecar `onnx` backend with the
build-time bake), and torch fp32 on CUDA through the GPU Compose overlay.

Laptop evidence existed for three of the four profiles on the 133-query
fixture: no reranker (MRR@10 0.657, rerank p50 —), torch CPU (MRR@10 0.697,
rerank p50 9.36s), and CUDA (MRR@10 0.697, rerank p50 435ms, peak VRAM
≈3.7 GiB). The quantized CPU route existed in code but had never been
measured end to end on the current build.

Before running the measurement, acceptance was pre-registered: switch the CPU
default to onnx-int8 only if MRR@10 and HitRate@5 are each within 0.01 of
torch AND rerank p50 is at most 2 seconds.

The full-corpus 133-query comparison (3 runs per query, committed report,
serving profile recorded in-report as backend `onnx-int8` on `cpu`) measured:

| Profile | HitRate@5 | MRR@10 | nDCG@5 | rerank p50/p95 |
|---|---|---|---|---|
| torch fp32 CPU | 0.812 | 0.697 | 0.709 | 9.36s / 18.6s |
| dynamic-int8 ONNX CPU | 0.820 | 0.725 | 0.731 | 5.42s / 9.70s |

Quality passed the pre-registered bar — both headline metrics landed at or
above torch. Latency failed it: rerank p50 5.42s is well above the 2s bar and
only ≈1.7× faster than torch, far from the order-of-magnitude improvement the
quantization literature suggests for this export path on this hardware
(12 cores, no onnxruntime session tuning).

## Decision

Keep the CPU default at torch fp32. The pre-registered latency bar was not
met, so no configuration change is made: the sidecar `RERANKER_BACKEND`
default remains `torch`, and the GPU Compose overlay remains the documented
profile for latency-sensitive deployments (identical quality at rerank p50
435ms).

## Consequences

- The int8 route stays available (`RERANKER_BACKEND=onnx` plus the bake), and
  its quality result removes the accuracy concern; only serving-level tuning
  (onnxruntime session and graph-optimization options) remains as a possible
  future revisit. Any such revisit requires a NEW pre-registered measurement
  with fresh thresholds — these thresholds are spent.
- This record corrects a stale phrase in ADR 0003: under Compose the int8
  bake is opt-in (`RERANKER_BAKE_QUANTIZED=0` default), not the built
  default, and the serving-backend default is `torch`.
- The evaluator report now records the serving reranker profile
  (model/backend/device) so committed evidence is self-describing.
- The generation-judge gate in TODO stays open for a separate decision; this
  record settles only the retrieval profile.
