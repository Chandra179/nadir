# 0018 — Restore a repeatable Retrieval evaluation Module

- **Status:** Accepted
- **Date:** 2026-09-12
- **Deciders:** Chandra, Codex
- **Supersedes:** [0010](0010-remove-offline-eval-harness.md)

## Context

The product's core value is document-grounded Retrieval and answer quality.
The repository retained a 34-query golden set and historical reports, but the
old evaluation command was removed after it drifted from the current search
Interface and duplicated obsolete configuration fallback logic. Without a
repeatable runner, Retrieval changes cannot be compared against a fresh local
run.

## Decision

Restore a development-only `cmd/evalbench` command backed by an
`internal/eval` Module. The evaluator uses the current `search.Query` seam,
loads the existing JSON golden set, runs every query repeatedly, bypasses the
semantic cache, and writes the existing aggregate metrics and per-query
latencies as JSON.

The command constructs the same Qdrant, embedder, optional reranker, chunker,
document-intake, and indexing Adapters used by the server. `--ensure-ingest`
uses the current source discovery and explicit configuration, including
host/container path matching in golden results. It never changes the running
server or becomes a runtime dependency.

## Consequences

- Retrieval changes have a repeatable local measurement path again.
- `--no-rerank` provides a comparable reranker control; cache is always
  bypassed so cache state does not determine the score.
- Reports remain directional until the golden set grows beyond the sample
  corpus and generation faithfulness is evaluated separately.
- The evaluator is an additional development entrypoint, not a production
  server process or distributed coordinator.
