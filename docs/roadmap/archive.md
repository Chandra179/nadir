# Completed roadmap archive

This document preserves completed implementation work and historical retrieval
measurements. The active backlog lives in [`TODO.md`](../../TODO.md).

## Retrieval baseline

The committed benchmark used a 34-query golden set over `samples/` with
`top_k=5`. It is a regression fixture, not a production-quality release gate.

| Stage | HitRate@5 | MRR@10 | nDCG@5 | p50 latency |
|---|---:|---:|---:|---:|
| Baseline (MiniLM reranker, un-prefixed index) | 0.824 | 0.689 | 0.742 | 201ms |
| Nomic prefixes + BM25 contextual alignment | 0.853 | 0.707 | 0.762 | 190ms |
| BGE reranker v2 M3 sidecar | **0.882** | **0.824** | **0.857** | 3206ms |
| BGE reranker v2 M3 with torch-int8 | 0.824 | 0.765 | 0.798 | 3372ms |
| Hybrid search without reranking | — | 0.740 | 0.784 | 49ms |
| HyPE enabled on the toy corpus | 0.882 | 0.804 | 0.823 | ~flat |

The reports are retained under `test/evaluation/reports/`. The small golden set
and reranker latency are still active constraints; follow-up work remains in
`TODO.md`.

The repeatable evaluator loads the golden set, bypasses semantic cache for
comparability, supports reranker control, repeats latency samples, and writes
JSON reports.

## Phase 1 — Cheap retrieval wins

- Added a swappable reranker model through `reranker.model` and
  `RERANKER_MODEL`; the default was upgraded from MiniLM to
  `BAAI/bge-reranker-v2-m3`.
- Added Nomic query/document task prefixes at the embedding call sites.
- Aligned the BM25 sparse leg with the contextual text used by dense indexing.
- Recorded that the index requires a reindex after changing prefixes or the
  embedding configuration.

## Phase 2 — Index-time enrichment

- Added the `internal/knowledge/enrichment` seam and Ollama-backed enrichment.
- Added the `enrichment.hype.enabled` flag and deterministic HyPE sibling point
  identifiers.
- Added the `enrichment.contextual.enabled` flag for LLM-written situational
  context.
- Added `HYPE_ENABLED` and `CONTEXTUAL_ENABLED` environment overrides.
- Evaluated enrichment on the toy corpus; it remains disabled by default until
  a fragmented, representative corpus demonstrates a benefit.

## Phase 2.5 — Production hardening

- Added bounded, insertion-ordered Chat event retention with explicit replay
  resynchronization behavior.
- Added request and ingestion budgets for uploads, source files, queries,
  fragments, embeddings, chunks, and `top_k`.
- Made semantic-cache reads filter-aware and version-aware.
- Added Qdrant schema validation, startup failure propagation, and collection
  setup timeouts.
- Serialized Chat history mutations and added paged history reads.
- Removed tracked build artifacts and documented the single-node retention
  decision in ADR-0013.
- Added portable CPU Compose deployment plus an optional NVIDIA override for
  Linux and Windows WSL2; Apple Silicon uses the CPU reranker.

## Phase 2.6 — Architecture deepening

- Centralized explicit LLM endpoint configuration and optional Docling config.
- Separated Retrieval-owned request/result types from transport and storage
  types.
- Split indexing into per-file planning and replacement commit stages.
- Centralized shared Qdrant clients, collection provisioning, codecs, and point
  ID decoding under the Qdrant adapter.
- Wired optional PDF intake through the Docling adapter while preserving the
  original PDF path as source identity.

## Phase 2.7 — Frontend contract migration

- Replaced server-rendered HTMX/Alpine templates with the React, TypeScript,
  Tailwind, and Vite dashboard.
- Replaced HTML fragments and legacy routes with versioned JSON/SSE contracts.
- Preserved in-place edit pruning, SSE replay, cancellation, document upload and
  reset, and Chat deletion.
- Kept the dashboard as a separate local frontend application.
- Added Vitest and React Testing Library coverage for result rendering, copy
  behavior, and EventSource reconnect behavior.

## Phase 2.8 — Bounded-context architecture refactor

- Kept executable entrypoints under `cmd/api` and `cmd/evaluator`.
- Grouped private Go code under `knowledge`, `retrieval`, `conversation`, and
  `evaluation` contexts.
- Moved transport and external systems under `internal/transport/http` and
  `internal/adapters`.
- Moved configuration, logging, observability, middleware, and composition
  under `internal/platform`.
- Grouped deployment ownership under `deploy/` and deferred unsupported
  distributed manifests.
- Split dashboard ownership into application, feature, workspace, shared HTTP,
  hooks, and styles layers.
- Recorded the ownership and migration decision in ADR-0022.

## Completed P0 work

- Made Document replacement failure-safe through staged version publication.
- Made Chat edits and delete-all operations safe against concurrent generation.
- Made full Document reset recoverable through versioned Qdrant collection
  publication.
- Added fault-injection coverage for failed staging, publication, and cleanup.
- Serialized overlapping indexing passes within one process.
- Made semantic-cache invalidation generation-aware within one process.

## Completed P1 and P2 work

- Added end-to-end HTTP smoke coverage for ingestion, Retrieval, SSE replay,
  cancellation, edit/delete routing, delete-all Chats, reset, and the main
  success path.
- Reconciled user documentation and ADRs with in-place editing, delete-all
  Chat, explicit configuration, and the single-node event log.
- Added deterministic browser coverage for dashboard startup, streaming,
  uploads, cancellation, edit-tail pruning, Chat deletion, and reset.
- Added and enforced the dashboard lockfile and CI checks for typechecking,
  linting, tests, static build, and Playwright.
- Narrowed consumer-owned capability seams and removed redundant Generator and
  cache-provisioning contracts under ADR-0020.

## Completed P1 work

- Made Chat shutdown drainable: active generations are cancelled, generation
  supervisors are awaited, and detached history writes finish before shared
  resources close. A shutdown timeout is surfaced to the lifecycle logger.
- Added regression coverage for blocked detached persistence and shutdown
  cancellation of an active generation with partial-answer persistence.
- Added separate liveness and readiness endpoints. Readiness checks Qdrant,
  embedding capability and configured vector dimensions, and the enabled
  reranker sidecar's loaded model, backend, and device. Dependency failures
  return HTTP 503 with diagnostics, while liveness remains dependency-free.

Distributed leases, fencing, shared event storage, and cross-instance ordering
remain deliberately open work. See [`SCALING.md`](../SCALING.md) and the active
backlog.
