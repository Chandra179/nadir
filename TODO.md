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

Reports in `test/evaluation/reports/`. Rerank CPU latency (~3.2s p50) exceeds the
1–2s budget → quantized/base model swap listed under P2.

## Phase 0 — Eval foundation

- [x] Golden set: `test/evaluation/golden.json` (34 queries over `samples/`) with
      committed run reports in `test/evaluation/reports/`
- [x] Repeatable Retrieval evaluator: `go run ./cmd/evaluator` loads the golden
      set, bypasses semantic cache, supports reranker control, repeats latency
      samples, and writes comparable JSON reports.
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

- [x] `internal/knowledge/enrichment` — `Enricher` interface (in
      `interface.go`, the single definition; indexing imports it), Ollama chat
      client, lenient
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
      point-ID decoding live under `internal/adapters/qdrant`; document,
      history, and semantic-cache lifecycles remain separate.
- [x] Wired optional PDF document intake through the Docling HTTP Adapter for
      uploads and configured source paths; the original PDF path remains the
      source identity.

## Phase 2.7 — Frontend contract migration ✅

- [x] Replace server-rendered HTMX/Alpine templates with a React + TypeScript +
      Tailwind dashboard under `web/dashboard`.
- [x] Replace HTML fragments and legacy routes with versioned JSON/SSE API
      contracts; document them in `contracts/` and ADR-0021.
- [x] Keep in-place edit pruning, SSE cursor replay, cancellation, document
      upload/reset, and chat deletion in the new client.
- [x] Keep the React dashboard as a separate local Vite application; backend
      Compose runs Qdrant, the reranker, and the Go API only.
- [x] Add Vitest/React Testing Library coverage for safe result rendering,
      copy behavior, and native EventSource cursor/reconnect lifecycle.

## Phase 2.8 — Bounded-context architecture refactor ✅

- [x] Keep executable entrypoints under `cmd/api` and `cmd/evaluator`; future
      indexer/admin processes remain deferred until they have real lifecycles.
- [x] Group private Go code under the target contexts: `internal/knowledge`,
      `internal/retrieval`, `internal/conversation`, and `internal/evaluation`.
- [x] Move HTTP concerns to `internal/transport/http` and external systems to
      `internal/adapters/{qdrant,ollama,reranker,docling}`.
- [x] Move configuration, logging, observability, middleware, and composition
      under `internal/platform`.
- [x] Group supported Compose assets and future Kubernetes/Helm deployment
      ownership under `deploy/`; keep unsupported distributed manifests
      explicitly deferred.
- [x] Split dashboard ownership into `app`, feature-owned API/types and
      components, a workspace composition layer, shared hooks/HTTP, and
      `styles` so Chat, history, and document work can proceed independently.
- [x] Record the ownership and migration decision in ADR-0022; no old package
      aliases or compatibility routes remain.

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
- [x] Make in-place Chat turn edits and delete-all chat operations safe against
      concurrent turn creation or generation. Chat owns serialized history
      mutations, session/global revision checks, and active-generation
      cancellation; see ADR-0017.
- [x] Make full Document reset recoverable if collection deletion or recreation
      fails: publish a freshly provisioned collection through a stable Qdrant
      active alias, then retire old generations after publication.
- [x] Add fault-injection tests proving failed staging/publishing preserves the
      previous active Document and failed cleanup is retryable without deleting
      the published generation.
- [x] Serialize overlapping Indexing passes within one process so two SHA
      snapshots cannot commit the same source in completion order; distributed
      workers still need leases/fencing.
- [x] Make semantic-cache invalidation generation-aware so detached cache writes
      from before ingest/reset cannot become valid again after Clear fails or
      completes out of order.

### P1 — Lifecycle and user-visible confidence

- [ ] Make detached chat-history persistence drainable during shutdown so a
      process stop cannot silently lose completed Chat turns.
- [ ] Add dependency readiness checks for the configured Ollama embedding path
      and reranker sidecar: verify model load/embed capability, report runner
      failures and loaded model identity, and keep readiness separate from
      liveness before accepting Retrieval traffic.
- [ ] Define an explicit local inference resource profile that prevents
      generator, embedder, and reranker GPU contention; bound Ollama residency
      and parallelism, document CPU-reranker mode, and do not rely on automatic
      GPU selection as the only policy.
- [x] Add end-to-end HTTP smoke tests for ingest, Retrieval, SSE replay and
      cancellation, in-place editing/deletion routing, delete-all chats, full
      document reset, and the main success path. Add dependency-backed failure
      cases as the real-service E2E profile grows.
- [ ] Grow the golden set from 34 sample queries to 100+ real queries with
      distractor pairs, multi-hop cases, and generation-faithfulness labels.
- [ ] Reconcile removed source files when configured source paths are intended
      to mirror the corpus; retain source versions when ingest is upload-only.
- [ ] Add restart/shutdown tests proving active Chat generation and pending
      history persistence either drain within the shutdown budget or report a
      durable retry state.
- [ ] Secure or disable the always-on profiling listener, and separate liveness
      from readiness checks for Qdrant, Ollama, and the reranker.
- [x] Reconcile user documentation and ADRs with the current in-place editing,
      delete-all chat, explicit configuration, and single-node event-log
      behavior.
- [x] Add deterministic browser-level Playwright coverage for dashboard open,
      chat streaming, upload, cancellation, edit-tail pruning, one/all chat
      deletion, and document reset.
- [ ] Add dependency-backed browser coverage for session replay/reconnect and
      full-service flows against Qdrant, Ollama, reranking, and Docling.
- [x] Add and enforce a committed dashboard package lockfile plus CI checks for
      the TypeScript typecheck, lint, tests, static build, and Playwright run.

### P2 — Production measurements and maintainability

- [x] Narrow consumer-owned capability seams for Document Retrieval,
      indexing, reset, history reads, and Chat persistence; remove redundant
      Generator and cache-provisioning contracts. See ADR-0020.
- [ ] Measure PDF document-intake latency, memory, timeout, and failure
      behavior against real documents in a production-like environment.
- [ ] Finish the reranker benchmark on representative hardware. Compare the
      current BGE v2 M3 CPU/GPU profiles, GTE multilingual reranker base,
      MiniLM L6, and quantized ONNX against quality, p50/p95 latency, RAM,
      VRAM, startup time, and throughput; the 34-query toy set is not
      sufficient for a production default.
- [ ] Benchmark EmbeddingGemma 300M quantized against the current Nomic
      embedder on the same corpus and golden set. Treat prompt-format changes,
      vector dimensions, index size, RAM/VRAM, and a full reindex as part of
      the experiment; do not change the default embedder without evidence.
- [ ] Add confidence-gated adaptive reranking: return the hybrid RRF result
      for high-confidence queries and invoke a reranker only when dense and
      lexical rankings disagree or the top-result margin is weak. Measure
      quality, rerank coverage, p50/p95 latency, and dependency load.
- [ ] Tune and calibrate dense/BM25/RRF fusion with offline golden-set
      evaluation, query-type thresholds, exact-match/header boosts, and
      deterministic score handling. Keep the current unreranked hybrid path as
      the low-resource baseline.
- [ ] Add generation-side evaluation for faithfulness, answer relevancy, and
      context precision/recall using a judge model larger than the model under
      test.
- [ ] Make the configured reranker model and the loaded sidecar model
      verifiable at startup so separately deployed settings cannot drift.
- [ ] Move the history sidebar page size and other operational limits into
      explicit configuration if they need operational tuning.
- [ ] Add global admission and backpressure for Retrieval fragments, reranking,
      generation, embedding, indexing, and destructive operations; current
      limits are per request or per process.
- [ ] Add domain-level metrics, traces, and structured operation IDs for
      Retrieval, Chat, Indexing, cache invalidation, and reset outcomes.
- [ ] Add a CI contract-drift check or OpenAPI code generation so the central
      TypeScript API mirror cannot diverge from `contracts/http/openapi.yaml`.
- [ ] Run Qdrant backup/restore drills and document recovery objectives before
      claiming high availability.
- [ ] Add load benchmarks for concurrent Chat streams, long-fragment Retrieval,
      and large Document ingestion with p50/p95/p99 and dependency saturation.

### P3 — Retrieval experiments and capacity work

- [ ] Add adaptive Retrieval fallback only after confidence, filter-miss, and
      unsupported-answer telemetry exists; measure coverage against noise.
- [ ] Revisit Qdrant quantization and embedder replacement only when corpus size
      or golden-set recall demonstrates a capacity or quality ceiling.
- [ ] Benchmark sentence-window versus recursive chunking before changing the
      default chunker.
- [ ] Prototype SPLADE-v3 as an optional learned-sparse retrieval leg behind
      an explicit feature flag. Measure query/document CPU time, RAM, sparse
      index size, nonzero-term counts, and quality against BM25; require a full
      reindex and keep it out of the default profile unless it improves the
      real golden set. SPLADE's 30,522-term vocabulary and model inference are
      not a lightweight replacement for BM25 on the current laptop.
- [ ] Prototype ColBERT-style late interaction with precomputed token vectors
      and MaxSim, preferably using Qdrant multivectors. Measure index growth,
      ingest throughput, RAM, query latency, and quality before considering it
      as a reranker or retrieval replacement.
- [ ] Collect explicit relevance and user-selection labels, then evaluate a
      small learned-to-rank model over dense score, BM25 score, RRF rank,
      metadata, exact-match, and position features. Compare linear/logistic
      and LambdaMART-style models; keep the model optional until labels and
      out-of-sample gains justify the added training/evaluation lifecycle.

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
