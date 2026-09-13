# 0014 — Deepened indexing, retrieval, and Qdrant seams

- **Status:** Accepted
- **Date:** 2026-09-09
- **Deciders:** Chandra, Codex architecture review

## Context

The single-node modular monolith had three sources of architectural friction:
the chat use-case consumed storage-owned retrieval values, the indexing pass
combined expensive planning with replacement writes, and the document store,
history, and semantic cache repeated Qdrant client and payload primitives.
PDF conversion also existed only as a standalone Docling workflow. Runtime
endpoint fallback rules made enabled LLM roles inherit another role's
address or model.

## Decision

- `internal/search` owns the caller-facing Retrieval request, result, and
  filter values. The Qdrant store and reranker retain their storage-side
  representation behind the Retrieval seam.
- `internal/ingest` plans one Document completely before committing its
  replacement points. Document intake converts PDFs to Markdown before the
  same indexing pass, while the original source path remains the source
  identity.
- `internal/qdrantutil` owns shared Qdrant clients, dense collection setup,
  primitive payload codecs, and point-ID decoding. Store, history, and
  semantic-cache Modules keep their own payload schemas and lifecycle rules.
- `config.Config` validates explicit role-specific Ollama endpoints and
  optional Docling settings once during loading. The static YAML plus
  environment override model from ADR-0009 remains unchanged; enabled LLM
  roles do not inherit another role's address or model.

Implementation paths were later reorganized under the bounded-context layout:
the current packages are `internal/retrieval/search`,
`internal/knowledge/indexing`, and `internal/adapters/qdrant/shared`.

## Consequences

- Chat and the HTTP transport no longer need to know storage-specific chunk
  fields or filter types.
- Expensive indexing work can be tested separately from replacement commit
  behavior, while retry and cache invalidation remain part of one indexing
  pass.
- Qdrant client/version changes have better locality without merging domains
  that only happen to share an infrastructure Adapter.
- PDF uploads and configured source PDFs use the same indexing behavior, but
  Docling conversion latency and memory must be measured in production-like
  environments.
- The in-process event log remains intentionally single-node per ADR-0013;
  horizontal scaling still requires a shared event backend later.
