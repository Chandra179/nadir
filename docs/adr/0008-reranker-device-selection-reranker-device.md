# 0008 — Reranker device selection: `RERANKER_DEVICE=auto|cpu|cuda`

- **Status:** Partially superseded by [0027](0027-local-inference-resource-profile.md)
- **Date:** 2026-09-06; deployment packaging revised 2026-09-09
- **Deciders:** Chandra, ZCode session

## Context

The reranker sidecar was CPU-only, but the dev laptop has an NVIDIA GPU
(RTX 4050) already used by Ollama. int8 is a CPU artifact (`torch-int8`
quantizes CPU linears), so only fp32 torch uses the GPU.

## Decision

`RERANKER_DEVICE` (env, default `auto`):

- **`cpu`** — force CPU; `RERANKER_BACKEND` selects fp32 torch or `torch-int8`.
- **`auto` / `cuda`** — on a CUDA-capable host, load fp32 torch
  `CrossEncoder(device="cuda")`. Backends other than `torch` are rejected on CUDA.

Packaging: `sidecars/reranker/Containerfile` takes a `GPU` build arg choosing
`requirements-gpu.txt` (CUDA torch) or `requirements-cpu.txt`. The base
`deploy/compose/compose.yaml` is CPU-safe with no NVIDIA reservation.
`deploy/compose/compose.gpu.yaml` is an explicit overlay for Linux or Windows
WSL2 that selects the CUDA image and adds the NVIDIA device reservation.

## Consequences

- On a CUDA host with the overlay, the reranker runs fp32 on CUDA and no
  longer occupies the CPU during searches.
- On macOS the reranker runs on CPU while host-side Ollama can use Metal.

## Current device policy (2026-10-03)

[0027](0027-local-inference-resource-profile.md) supersedes automatic device
selection for the local profile: CPU is explicit by default, CUDA needs a
measured GPU profile, and `auto` is permitted only with `inference.profile:
custom`. [0029](0029-ollama-scheduler-owns-llm-concurrency.md) changes Ollama
admission without changing that device policy.
