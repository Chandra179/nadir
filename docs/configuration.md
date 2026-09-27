# Configuration

`internal/bootstrap/configuration/config.yaml` is the external configuration contract. `internal/bootstrap/configuration` loads that file, applies the existing environment variable overrides, validates role-specific values, and supplies the internal Go configuration. The cutover preserved YAML names, environment names, defaults, and feature flags. Changing embedding task prefixes or enabling ingest enrichment requires a reindex.

| Group | Owns |
| --- | --- |
| `http`, `middleware`, `profiling` | API listener, request limits, logging, diagnostics |
| `source`, `ingest`, `chunking` | Document discovery, conversion, size limits, indexing batches |
| `qdrant`, `embedder` | Collection identity and embedding role |
| `search`, `semantic_cache`, `reranker` | Retrieval policy and optional reranking/cache |
| `generator`, `rewriter`, `history` | Conversation and persistence |
| `enrichment`, `docling` | Optional index-time LLM and PDF conversion |
| `inference`, `gates` | Ollama process settings; indexing/destructive operation gates |

The groups separate use cases reasonably well. `qdrant` and `embedder` are shared infrastructure, so they stay at the top level. The role-specific Ollama address and model fields deliberately remain in `generator`, `rewriter`, and each enrichment role; enabled roles must declare their own endpoints and models. The public file is somewhat long, but regrouping or renaming keys would break existing deployments without a measurable runtime benefit. New internal consumers should receive only the fields they need from bootstrap.

The default `inference.profile: local` delegates LLM/embedding concurrency to the Ollama scheduler (`OLLAMA_NUM_PARALLEL`) with per-role request timeouts and a finite `keep_alive`. The `gates` limits (indexing, destructive) are per process. `enrichment.contextual.enabled` defaults to false, affect ingestion only, and need reindexing to change existing documents. `search.fusion` remains opt-in. The active external key list and local/Compose address rules are in [AGENTS.md](../AGENTS.md).
