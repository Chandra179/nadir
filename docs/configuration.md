# Configuration

The [configuration YAML](../internal/bootstrap/configuration/config.yaml) is the
external configuration contract. `internal/bootstrap/configuration` loads it,
applies environment overrides, validates role-specific values, and supplies
the internal Go configuration. Keep existing keys compatible. Changing an
embedding model, dimensions or task prefixes, or enabling ingest enrichment,
requires a reindex.

| Group | Owns |
| --- | --- |
| `http`, `middleware`, `profiling` | API listener, request limits, logging, diagnostics |
| `documents`, `ingest`, `chunker` | Document discovery/reconciliation, conversion, size limits, chunking and indexing batches |
| `qdrant`, `embedder` | Collection identity and embedding role |
| `search`, `semantic_cache`, `reranker` | Retrieval policy and optional reranking/cache |
| `generator`, `chat`, `rewriter`, `history` | Answer generation, prompt/event budgets, follow-up rewriting and persistence |
| `enrichment`, `docling` | Optional index-time LLM and PDF conversion |
| `inference`, `gates` | Ollama process settings; indexing/destructive operation gates |

The groups separate use cases reasonably well. `qdrant` and `embedder` are shared infrastructure, so they stay at the top level. The role-specific Ollama address and model fields deliberately remain in `generator`, `rewriter`, and each enrichment role; enabled roles must declare their own endpoints and models. The public file is somewhat long, but regrouping or renaming keys would break existing deployments without a measurable runtime benefit. New internal consumers should receive only the fields they need from bootstrap.

The default `inference.profile: local` delegates LLM/embedding concurrency to the Ollama scheduler (`OLLAMA_NUM_PARALLEL`) with per-role request timeouts and a finite `keep_alive`. The `gates` limits (indexing, destructive) are per process. `enrichment.contextual.enabled` defaults to false, affects ingestion only, and requires reindexing to change existing documents. `search.fusion` remains opt-in. The active environment key list and local/Compose address rules are in [AGENTS.md](../AGENTS.md).
