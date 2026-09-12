# TODO

Retrieval-accuracy roadmap (audit + research cross-reference, Aug 2026; priority
and architecture re-audited Sep 2026). Each change is measured
against the golden set (HitRate@k / Recall@k / MRR@10 / nDCG@k) when it changes
Retrieval quality; correctness and lifecycle changes require focused regression
tests and production evidence instead.

## Measured results (34-query golden set over `samples/`, top_k=5)

| Stage | HitRate@5 | MRR@10 | nDCG@5 | p50 latency |
|---|---|---|---|---|
| Baseline (MiniLM reranker, un-prefixed index) | 0.824 | 0.689 | 0.742 | 201ms |
| + nomic prefixes + BM25 contextual align | 0.853 | 0.707 | 0.762 | 190ms |
| + bge-reranker-v2-m3 sidecar | **0.882** | **0.824** | **0.857** | 3206ms |
| same, torch-int8 quantized (Sep 2026 A/B) | 0.824 | 0.765 | 0.798 | 3372ms |
| same, no-rerank reference | — | 0.740 | 0.784 | 49ms |
| HyPE enabled on toy corpus | 0.882 | 0.804 | 0.823 | ~flat |

Reports in `tests/eval/reports/`. Rerank CPU latency (~3.2s p50) exceeds the
1–2s budget → quantized/base model swap listed under P2.

## Phase 0 — Eval foundation

- [x] Golden set: `tests/eval/golden.json` (34 queries over `samples/`) with
      committed run reports in `tests/eval/reports/`
- [ ] Grow golden set to 100+ queries as real corpus grows; keep distractor pairs

## Phase 1 — Cheap wins ✅

- [x] Swappable reranker model: `reranker.model` in config.yaml → `RERANKER_MODEL`
      env → sidecar. Default upgraded `ms-marco-MiniLM-L-6-v2` (~60 BEIR nDCG@10,
      can *hurt* vs no rerank per NVIDIA benchmark) → `BAAI/bge-reranker-v2-m3`
      (~71.5 BEIR, MIT). Revert via config; compose memory 1g→3g.
      Caveat: the Go client never pushes `reranker.model` to the sidecar — the
      loaded model is agreed via the shared `RERANKER_MODEL` env/default only.
- [x] nomic task prefixes at call sites: `embedder.query_prefix` ("search_query: ")
      on query fragments, `embedder.document_prefix` ("search_document: ") on
      ingest embeds. Empty string disables (any-model swappable).
- [x] BM25 sparse leg now indexes the same contextual text the dense leg
      embeds (path > header + body), instead of bare chunk text.
- [x] **Requires one reindex** after pulling: drop collection, re-ingest
      (`curl -X DELETE localhost:6333/collections/documents_chunks`).

## Phase 2 — Index-time LLM enrichment ✅ (flags default OFF)

Zero query-time latency; one-time Ollama cost per chunk at ingest. Enabling
after a prior ingest requires a reindex.

- [x] `internal/enrichment` — `Enricher` interface (in `interface.go`, the
      single definition; ingest imports it), Ollama chat client, lenient
      JSON parsing, graceful per-chunk degradation (warn + index without
      enrichment)
- [x] HyPE feature flag `enrichment.hype.enabled` (+ `questions_per_chunk`,
      default 3): hypothetical questions embedded as extra sibling points that
      carry the parent's identity fields → existing Key() dedup collapses them;
      ReplaceDocument sweeps them; point IDs get `:hype:<n>` suffix
- [x] Contextual retrieval flag `enrichment.contextual.enabled`: LLM-written
      situational intro prepended before embedding/indexing
- [x] Env overrides `HYPE_ENABLED`, `CONTEXTUAL_ENABLED`
- [x] A/B on toy corpus: neutral/slightly negative (tiny clean corpus, no
      context fragmentation for it to fix; gemma3:1b question noise). Keep OFF
      until a real corpus exists; re-eval there — paper gains (+20pp precision)
      show on fragmented/larger corpora.

## Phase 2.5 — Production hardening ✅

- [x] Bounded, insertion-ordered turn retention: the in-process broker keeps a
      bounded number of turn streams and replay bytes, evicts finished streams
      deterministically, and emits an explicit resync event when a cursor is
      older than the retained window. If all retained streams are active, a new
      generation is rejected instead of growing memory without bound.
- [x] Request and ingest budgets: bound upload bodies, source-file reads,
      query length, sentence fragments, concurrent fragment searches, embedding
      batches, chunks per file, and `top_k`.
- [x] Semantic-cache correctness: filtered searches bypass the unfiltered
      query cache, cache entries carry an embedding/configuration version, and
      cache hits are bounded to the requested result count.
- [x] Startup and schema safety: Qdrant vector schemas are validated rather
      than silently reused, startup failures are returned to `main`, and
      collection setup has a timeout.
- [x] History concurrency and pagination: turn writes/deletes are serialized
      through one bounded seam and history reads use paged Qdrant scans.
- [x] Cleanup and documentation: removed tracked build artifacts, aligned
      source discovery and local-run documentation, and recorded the retention
      decision in ADR-0013.
- [x] Portable Compose deployment: the base stack is CPU-safe with a source
      bind mount, container healthcheck, `.dockerignore`, and an explicit
      NVIDIA GPU override for Linux/Windows WSL2; Apple Silicon skips the AVX2
      quantized bake.

## Phase 2.6 — Architecture deepening ✅

- [x] Centralized explicit LLM endpoint configuration and optional Docling
      configuration in the validated config module; enabled roles no longer
      inherit another role's address or model.
- [x] Retrieval-owned request/result types keep chat and the HTTP transport
      independent from storage-specific chunk and filter types.
- [x] Split the indexing pass into per-file planning and replacement commit
      stages while preserving bounded workers, retries, and cache invalidation.
- [x] Shared Qdrant clients, collection provisioning, payload codecs, and
      point-ID decoding live in `internal/qdrantutil`; document, history, and
      semantic-cache lifecycles remain separate.
- [x] Wired optional PDF document intake through the Docling HTTP Adapter for
      uploads and configured source paths; the original PDF path remains the
      source identity.

## Priority backlog

This backlog is ordered by risk, not novelty. P0 protects data correctness and
destructive operations. P1 covers lifecycle reliability, user-visible
correctness, and evidence needed to make quality decisions. P2 covers
production measurements and maintainability. P3 contains experiments that
should wait for real usage data.

### P0 — Data correctness and destructive-operation safety

- [x] Make Document replacement failure-safe with versioned points: stage the
      new version, activate it, then deactivate and clean up older versions.
      Record the protocol in ADR-0016 and cover replacement visibility in the
      Store integration test.
- [ ] Make in-place Chat turn edits and delete-all chat operations safe against
      concurrent turn creation or generation. Define session mutation
      ownership/version checks so deleted or pruned turns cannot reappear.
- [ ] Make full Document reset recoverable if collection deletion or recreation
      fails; preserve a known-good collection or define restore/retry semantics.
- [ ] Add fault-injection tests proving failed staging preserves the previous
      active Document and failed cleanup is retryable without duplicate active
      versions.

### P1 — Lifecycle and user-visible confidence

- [ ] Make detached chat-history persistence drainable during shutdown so a
      process stop cannot silently lose completed Chat turns.
- [ ] Add end-to-end HTTP smoke tests for ingest, Retrieval, SSE replay and
      cancellation, in-place editing, single-chat deletion, delete-all chats,
      full document reset, and error responses.
- [ ] Grow the golden set from 34 sample queries to 100+ real queries with
      distractor pairs, multi-hop cases, and generation-faithfulness labels.
- [ ] Secure or disable the always-on profiling listener, and separate liveness
      from readiness checks for Qdrant, Ollama, and the reranker.
- [x] Reconcile user documentation and ADRs with the current in-place editing,
      delete-all chat, explicit configuration, and single-node event-log
      behavior.

### P2 — Production measurements and maintainability

- [ ] Measure PDF document-intake latency, memory, timeout, and failure
      behavior against real documents in a production-like environment.
- [ ] Finish the reranker benchmark on representative hardware. Compare the
      current CPU profile, smaller models, and quantized ONNX against quality,
      p50/p95 latency, and memory; the 34-query toy set is not sufficient for a
      production default.
- [ ] Add generation-side evaluation for faithfulness, answer relevancy, and
      context precision/recall using a judge model larger than the model under
      test.
- [ ] Make the configured reranker model and the loaded sidecar model
      verifiable at startup so separately deployed settings cannot drift.
- [ ] Move the history sidebar page size and other operational limits into
      explicit configuration if they need operational tuning.

### P3 — Retrieval experiments and capacity work

- [ ] Add adaptive Retrieval fallback only after confidence, filter-miss, and
      unsupported-answer telemetry exists; measure coverage against noise.
- [ ] Revisit Qdrant quantization and embedder replacement only when corpus size
      or golden-set recall demonstrates a capacity or quality ceiling.
- [ ] Benchmark sentence-window versus recursive chunking before changing the
      default chunker.

### Deliberately deferred

- [ ] Add a shared event backend such as Redis Streams only when multiple Nadir
      instances are deployed. It then also requires subscriber routing and an
      SSE affinity/deployment decision; the current broker remains intentionally
      single-node under ADR-0013.
- [ ] CRAG, speculative RAG, Adaptive-RAG routing, GraphRAG, and multi-query
      expansion remain research candidates, not implementation priorities.

## Rejected (research says skip for this domain)

- HyDE / multi-query expansion — underperforms vanilla hybrid retrieval for
  precise numeric/entity queries (T2-RAGBench 2026); fragment-splitting
  already captures part of the benefit
- GraphRAG / RAPTOR — heavy machinery, unjustified at current corpus size
- Late chunking — efficiency win but sacrifices relevance/completeness vs
  contextual retrieval (arXiv 2504.19754); needs long-context pooling support
