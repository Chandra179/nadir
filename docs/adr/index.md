# Architecture Decision Records

Decisions are recorded in [Nygard format](https://adr.github.io/) — Status,
Context, Decision, Consequences — one decision per file, numbered
`NNNN-kebab-case.md`. Superseded records stay and point at their successor.

| # | Decision | Status |
|---|----------|--------|
| [0001](0001-enricher-interface-single-home.md) | The `Enricher` interface lives in `internal/enrichment`, next to its implementation; consumers import it, no compat shims | Accepted |
| [0002](0002-conversational-query-rewriting.md) | Conversational follow-ups are rewritten into standalone queries before retrieval and generation (`internal/rewriter`, Rewrite-Retrieve-Read) | Accepted |
| [0003](0003-reranker-int8-quantization-two-routes.md) | Reranker sidecar int8 quantization via `RERANKER_BACKEND`: build-time-baked ONNX (default) or startup torch-int8, fp32 fallback chain | Accepted |
| [0004](0004-torch-int8-bypasses-sentence-transformers.md) | The `torch-int8` backend uses a minimal `TorchInt8Reranker` — sentence-transformers' predict dispatch breaks under `quantize_dynamic` | Accepted |
| [0005](0005-fresh-control-evalbench-ab.md) | Retrieval A/Bs run a fresh control alongside the treatment in the same session; stored reports are context, not baselines | Accepted |
