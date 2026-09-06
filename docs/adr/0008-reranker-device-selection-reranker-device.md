# 0008 — Reranker device selection: `RERANKER_DEVICE=auto|cpu|cuda`

- **Status:** Accepted
- **Date:** 2026-09-06
- **Deciders:** Chandra, ZCode session

## Context

The reranker sidecar was CPU-only by design (ADR 0003), but the dev laptop
has an NVIDIA GPU (RTX 4050) already used by Ollama. Constraints discovered:

- The int8 routes are **CPU artifacts**: the baked ONNX export is an AVX2
  dynamic-int8 graph and `torch-int8` quantizes CPU linears. Serving them on
  CUDA is meaningless; only fp32 torch meaningfully uses the GPU.
- `optimum[onnxruntime-gpu]==2.3.0` is broken upstream (the optimum/onnx
  split conflicts) — rejected as an ONNX-on-GPU route.

## Decision

`RERANKER_DEVICE` (env, default `auto`):

- **`cpu`** — force CPU; the ADR 0003 route chain (baked int8 → fp32 onnx →
  torch-int8 → torch) applies unchanged.
- **`auto` / `cuda`** — on a CUDA-capable host, load fp32 torch
  `CrossEncoder(device="cuda")` regardless of `RERANKER_BACKEND`, logging a
  notice that the int8 routes are CPU-only.

Packaging: `services/reranker/Dockerfile` takes a `GPU` build arg choosing
`requirements-gpu.txt` (CUDA torch) vs `requirements-cpu.txt`; the bake still
runs in both images so `RERANKER_DEVICE=cpu` keeps the int8 artifact. The
compose stack is GPU-first in a single `docker-compose.yml`: the CUDA build
(`RERANKER_GPU`, default 1) and the NVIDIA device reservation are always
declared, with `RERANKER_DEVICE=cuda` as default — the primary host has the
toolkit, and switching to CPU is `RERANKER_GPU=0 RERANKER_DEVICE=cpu` (the
device switch needs no rebuild). Hosts without the NVIDIA toolkit must delete
the `reservations` block, since Compose cannot conditionalize a structural
block on an env var; a base+overlay split was rejected to avoid duplicating
the model build args across files.

## Consequences

- On the laptop, the reranker runs fp32 on CUDA: no int8 speedup, but the
  cross-encoder no longer owns the CPU during searches.
- The baked int8 export stays relevant for CPU deployments; a swapped model
  still degrades to fp32 (ADR 0003).
- CUDA + int8 remains unimplemented until upstream `optimum[onnxruntime-gpu]`
  is usable.
