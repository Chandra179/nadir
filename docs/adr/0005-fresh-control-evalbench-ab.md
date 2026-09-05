# 0005 — Retrieval A/Bs always run a fresh control alongside the treatment

- **Status:** Accepted
- **Date:** 2026-09-05
- **Deciders:** Chandra, ZCode session

## Context

TODO.md's standing rule is to measure every retrieval change with
`cmd/evalbench` before and after. The stored reports, however, age badly:

- Re-running the unchanged fp32 bge-reranker-v2-m3 configuration today
  produced MRR@10 0.793 / nDCG@5 0.826, while the August report for the same
  configuration recorded 0.824 / 0.857. Nothing in the config changed;
  index state, candidate ordering ties, and environment drift between
  sessions are enough to move a 34-query golden set by several points.
- Treating the old report as "before" would have mis-attributed that drift
  to the change under test (here: the int8 quantized reranker).

A second trap surfaced while automating the A/B: the treatment leg was
accidentally pointed at the stopped fp32 sidecar's address instead of the
int8 sidecar's, and because `search.rerankTopK` degrades to un-reranked
results on reranker failure, the run silently reported the *no-reranker*
baseline (p50 39ms was the tell) while claiming to measure int8.

## Decision

- Every evalbench A/B runs the **control and treatment back-to-back in the
  same session** (same index, same machine state), writing both reports —
  e.g. `rerank_ab_fp32_control.json` and `rerank_ab_torch_int8.json`.
  Stored reports from earlier sessions are context, not baselines.
- Both legs are launched as detached units (systemd user units) so long
  runs survive interactive tooling, and each leg gets an explicit
  `RERANKER_ADDR` (or equivalent) — never an inherited default that may
  point at a different sidecar than intended.
- Before accepting a treatment result, sanity-check it against the known
  latency floor: the reranker cannot be faster than its model allows; a
  reranked run that comes back at no-rerank speed means the reranker was
  never called, not that quantization was miraculous.

## Consequences

- Comparisons measure the change, not the calendar: the quantization A/B
  verdict (−2.8pp MRR for 1.29× on torch-int8) was only trustworthy because
  both legs ran minutes apart on the same index.
- Golden-set noise is still ±~3pp on 34 queries; treat deltas of that size
  as inconclusive and grow the golden set (TODO Phase 0) before making
  quality-critical swaps on small deltas.
- Costs one extra eval run per change (minutes), which is cheap against
  shipping a regression justified by a stale baseline.
