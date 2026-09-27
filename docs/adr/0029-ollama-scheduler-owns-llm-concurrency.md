# 0029 — Ollama's scheduler owns LLM and embedding concurrency

- **Status:** Accepted
- **Date:** 2026-09-26
- **Deciders:** Chandra

## Context

ADR-0027 introduced a shared process-local Ollama Gate plus per-operation
admission budgets for retrieval, reranking, generation, embedding, indexing,
and destructive mutations. The intent was to keep concurrent work from
overlapping on one local GPU/CPU.

In practice the client-side gate double-limits the model: Ollama already
serializes and queues requests per `OLLAMA_NUM_PARALLEL` slots. A client gate
therefore only converts that internal queue into a second waiting line that
fails with `inference capacity exhausted` after a fixed timeout. It also
imposes one-at-a-time defaults (`INFERENCE_OLLAMA_MAX_CONCURRENT=1`) that
prevent Ollama's own parallel scheduling from ever being used, and it cannot
coordinate anything outside the Ollama process anyway.

Three other limits remain useful because the resource they guard is not
Ollama:

- the reranker sidecar has its own bounded concurrency and rejects overflow
  with HTTP 429; a client-side queue in front of it preserves reranking
  quality under burst instead of degrading to un-reranked results immediately;
- indexing must stay single-writer; without a finite queue a concurrent ingest
  request would block on the process mutex for a whole sweep;
- destructive mutations (reset, session/document deletion) must not race.

## Decision

- All client-side Ollama gates and the `Generation`, `Embedding`, and
  `Reranking`/`Retrieval` admission operations are removed. Concurrency for
  embedding, rewriting, HyPE/contextual enrichment, and streaming generation is
  owned by the Ollama scheduler (`OLLAMA_NUM_PARALLEL`) and bounded by each
  role's `request_timeout` and the request context.
- `INFERENCE_OLLAMA_MAX_CONCURRENT` and `INFERENCE_OLLAMA_QUEUE_TIMEOUT` are
  removed; `INFERENCE_OLLAMA_KEEP_ALIVE` stays.
- `ADMISSION_RETRIEVAL_*`, `ADMISSION_RERANKING_*`, `ADMISSION_GENERATION_*`,
  and `ADMISSION_EMBEDDING_*` are removed.
- `ADMISSION_INDEXING_*` and `ADMISSION_DESTRUCTIVE_*` stay and are the only
  operations in `internal/bootstrap/resources` admission control.
- The reranker keeps its explicit client-side gate
  (`RERANKER_MAX_CONCURRENT` / `RERANKER_QUEUE_TIMEOUT`) in front of the
  sidecar's own 429 rejection.
- Request-level `keep_alive`, explicit reranker device/backend policy, and the
  conservative CPU defaults from ADR-0027 are unchanged.

This decision supersedes the client-side Ollama gate and the per-operation
LLM admission aspects of ADR-0027. Its device-selection and keep-alive
decisions remain in force.

## Consequences

LLM calls no longer fail fast with a capacity error; under saturation they
queue inside Ollama, so many concurrent HTTP requests can wait there. Each
request is still bounded by its role timeout and context deadline. Operators
who want client-side load shedding can reintroduce it at the edge rather than
inside every provider adapter. The `admission.indexing.*` and
`admission.destructive.*` gauges remain on `/debug/metrics`; the removed
operations no longer report gauges.
