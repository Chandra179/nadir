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
| [0005](0005-fresh-control-evalbench-ab.md) | Retrieval A/Bs run a fresh control alongside the treatment in the same session; stored reports are context, not baselines | Superseded by 0010 |
| [0006](0006-chat-streams-over-domain-owned-event-log.md) | Chat turns stream over a domain-owned event log: typed generator events, supervisor-owned generation, cursor-replayable subscribers, SSE adapter, cancel command | Accepted |
| [0007](0007-dashboard-plain-eventsource-no-htmx-sse-extension.md) | The dashboard streams via a plain EventSource and a send/stop composer button; htmx's sse extension is removed (its npm dist is stale htmx-1 code) | Accepted |
| [0008](0008-reranker-device-selection-reranker-device.md) | Reranker device via `RERANKER_DEVICE` (auto\|cpu\|cuda): CUDA serves fp32 torch; int8 routes stay CPU-only | Accepted |
| [0009](0009-remove-runtime-settings-panel.md) | The runtime settings panel is removed; config is `config.yaml` + env, applied once at startup | Accepted |
| [0010](0010-remove-offline-eval-harness.md) | The offline eval harness (`cmd/evalbench`, `internal/eval`, `config/golden`) is removed | Accepted |
| [0011](0011-semantic-cache-naming-and-package.md) | The query-level vector cache is `cache.SemanticCache` in its own package, separate from `internal/store` | Accepted |
