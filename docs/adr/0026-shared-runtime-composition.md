# 0026 — Shared executable runtime composition

- **Status:** Accepted
- **Date:** 2026-09-13
- **Deciders:** Chandra, Codex architecture review

## Context

The API server and the Retrieval evaluator both needed the same Qdrant,
embedding, indexing, semantic-cache, and reranking graph. Maintaining that
graph independently made configuration drift likely and allowed tests and
production to exercise different seams.

## Decision

Create one platform Runtime composition module for the shared infrastructure
graph. Executables receive capability seams for retrieval, indexing, reset,
statistics, health probes, and shutdown. The API server adds chat, history,
generation, readiness, and HTTP lifecycle around that graph; the evaluator
uses it without starting an HTTP listener.

The Runtime owns one Qdrant connection and injects one process-local document
lifecycle coordinator into indexing. It does not own domain policy and does
not expose adapter implementations.

## Consequences

- API and evaluator use the same production composition and collection setup.
- Qdrant connection ownership and shutdown are explicit and centralized.
- Optional evaluator behavior is expressed as Runtime options instead of a
  second dependency graph.
- The Runtime remains a single-node composition boundary; distributed leases,
  event storage, and worker coordination remain future infrastructure work.
