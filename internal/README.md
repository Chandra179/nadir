# Go backend packages

The backend has three use-case groups under `core/`: `documents/`, `retrieval/`, and `conversation/`. `core/embedding/` defines the shared embedding capability and `core/observability/` contains provider-neutral operation correlation and bounded metrics. Conversation owns follow-up query rewriting because it consumes session history and calls retrieval with the rewritten query.

`edge/http/` maps the dashboard's versioned JSON and SSE contract. `providers/` implements Qdrant, Ollama, reranker, and Docling protocols. `bootstrap/` maps external config, creates process-wide resource budgets, composes providers with core use cases, and owns the HTTP lifecycle. The root Python `eval/` measures quality through the public API with Ragas.

Core packages must not import edge handlers, provider implementations, or bootstrap server lifecycle. Bootstrap is the only package that wires the full object graph. External YAML and environment keys stay compatible through `bootstrap/configuration`.
