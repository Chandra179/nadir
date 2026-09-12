# TODO

This file contains the active engineering backlog and current Retrieval
evaluation baseline. Completed work is preserved in
[`docs/roadmap/archive.md`](docs/roadmap/archive.md).

Changes that affect Retrieval quality are measured against the golden set
(HitRate@k / Recall@k / MRR@10 / nDCG@k). Correctness and lifecycle changes
require focused regression tests and production evidence.

## Current evaluation baseline

The current results use a 34-query golden set over `samples/`, with `top_k=5`.
This is a regression fixture, not evidence that generated answers are faithful
or that the system is ready for production-scale quality decisions.

| Stage | HitRate@5 | MRR@10 | nDCG@5 | p50 latency |
|---|---:|---:|---:|---:|
| Baseline (MiniLM reranker, un-prefixed index) | 0.824 | 0.689 | 0.742 | 201ms |
| Nomic prefixes + BM25 contextual alignment | 0.853 | 0.707 | 0.762 | 190ms |
| BGE reranker v2 M3 sidecar | **0.882** | **0.824** | **0.857** | 3206ms |
| BGE reranker v2 M3 with torch-int8 | 0.824 | 0.765 | 0.798 | 3372ms |
| Hybrid search without reranking | — | 0.740 | 0.784 | 49ms |
| HyPE enabled on the toy corpus | 0.882 | 0.804 | 0.823 | ~flat |

Reports are retained under `test/evaluation/reports/`. Reranker CPU latency is
above the 1–2 second target, and the golden set must grow before selecting a
new production default.

## Active priority backlog

This backlog is ordered by risk, not novelty. P0 protects data correctness and
destructive operations. P1 covers lifecycle reliability, user-visible
correctness, and evidence needed to make quality decisions. P2 covers
production measurements and maintainability. P3 contains experiments that
should wait for real usage data.

### P0 — Data correctness and destructive-operation safety

No open P0 items. Completed P0 work is preserved in
[`docs/roadmap/archive.md`](docs/roadmap/archive.md).

### P1 — Lifecycle and user-visible confidence

- [ ] Define an explicit local inference resource profile that prevents
      generator, embedder, and reranker GPU contention; bound Ollama residency
      and parallelism, document CPU-reranker mode, and do not rely on automatic
      GPU selection as the only policy.
- [ ] Grow the golden set from 34 sample queries to 100+ real queries with
      distractor pairs, multi-hop cases, and generation-faithfulness labels.
- [ ] Reconcile removed source files when configured source paths are intended
      to mirror the corpus; retain source versions when ingest is upload-only.
- [ ] Add integration restart/shutdown tests for the HTTP server and external
      history store, proving active Chat generation and pending history
      persistence either drain within the shutdown budget or report a durable
      retry state; unit drain coverage now exists.
- [ ] Secure or disable the always-on profiling listener and document the
      protected operational access path.
- [ ] Add dependency-backed browser coverage for session replay/reconnect and
      full-service flows against Qdrant, Ollama, reranking, and Docling.

### P2 — Production measurements and maintainability

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
  precise numeric/entity queries; fragment-splitting already captures part of
  the benefit.
- GraphRAG / RAPTOR — heavy machinery, unjustified at current corpus size.
- Late chunking — efficiency win but sacrifices relevance/completeness versus
  contextual retrieval; it also needs long-context pooling support.
