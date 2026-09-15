# TODO

This file contains the active engineering backlog and current Retrieval
evaluation baseline. Completed work is preserved in
[`docs/roadmap/archive.md`](docs/roadmap/archive.md).

Changes that affect Retrieval quality are measured against the golden set
(HitRate@k / Recall@k / MRR@10 / nDCG@k). Correctness and lifecycle changes
require focused regression tests and production evidence.

## Current evaluation baseline

Historical reports use the original 34-query golden set over `samples/`, with
`top_k=5`. The active fixture now contains 133 expert-authored synthetic
user-intent queries over the sample corpus. Both remain regression fixtures,
not evidence that generated answers
are faithful or that the system is ready for production-scale quality
decisions.

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

Archived as complete. See
[`docs/roadmap/archive.md`](docs/roadmap/archive.md#archived-p1-scope--2026-09-14).
The committed synthetic fixture remains a regression fixture; consent-safe
production data and expert judgments are external release evidence, not
fabricated repository content.

### P2 — Production measurements and maintainability

- [x] Add a repeatable Docling benchmark harness with health checks,
      per-document failures/timeouts, p50/p95 latency, and optional PID/Docker
      RSS sampling. Implemented by `scripts/benchmark_docling.py`.
- [x] Run the Docling benchmark against a consent-safe baseline PDF corpus in
      a production-like local process and record latency, memory, timeout, and
      failure evidence in
      [`test/evaluation/reports/docling-system-corpus-20260913.json`](test/evaluation/reports/docling-system-corpus-20260913.json).
      The six-document corpus was measured with three runs per document, a
      120-second request timeout, and RSS sampling: 18/18 successes, 0
      failures, 0 timeouts, p50 2.616s, p95 25.939s, and peak RSS 3,522,351,104
      bytes (~3.28 GiB). The run used the installed repository `venv` because
      the Docker build could not resolve PyPI; it is therefore a local process
      baseline, not a container/cgroup capacity result.
- [x] Run the available current-host reranker profiles with the same 133-query
      fixture, including BGE v2 M3 CPU/GPU, MiniLM Torch, MiniLM ONNX, and
      MiniLM dynamic-int8 ONNX. Record startup, p50/p95 latency, RSS, VRAM,
      throughput, ranking quality, failures, and timeouts in
      [`reranker-profile-comparison-20260915.json`](test/evaluation/reports/reranker-profile-comparison-20260915.json).
      This validates the benchmark path but is not a release decision because
      the fixture is synthetic and GTE is unavailable.
- [ ] Complete the representative-hardware reranker comparison. Compare the
      current BGE v2 M3 CPU/GPU profiles, GTE multilingual reranker base,
      MiniLM L6, and quantized ONNX against quality, p50/p95 latency, RAM,
      VRAM, startup time, and throughput; the sample-derived set is not
      sufficient for a production default. `scripts/benchmark_reranker.py`
      now provides a health-checked direct benchmark with graded ranking
      metrics, p50/p95 latency, sequential throughput, failures/timeouts, and
      optional PID/Docker RSS plus process-specific `nvidia-smi` VRAM sampling;
      it can resolve the existing golden fixture with `--corpus-dir` and now
      supports `--require-release-gate` to reject synthetic or incomplete
      production evidence. `scripts/benchmark_reranker_profiles.py` now owns
      startup/shutdown and records startup latency/RSS, loaded backend, p50/p95
      latency, RAM, throughput, ranking quality, failures, and timeouts. On
      the current host, BGE v2 M3 Torch, MiniLM L6 Torch, MiniLM L6
      ONNX, and MiniLM L6 dynamic-int8 ONNX each completed 133/133 queries
      with zero failures/timeouts. The measured p50/p95 latencies were
      561/2,927ms, 30/165ms, 16/114ms, and 11/75ms; peak RSS was 2.34, 0.88,
      1.00, and 0.96 GiB respectively. The current RTX 4050 BGE GPU run
      completed 133/133 queries with p50/p95 30/132ms, startup 5.79s, peak
      RSS ~1.37 GiB, and peak VRAM ~2.37 GiB. The compact comparison is recorded in
      [`reranker-profile-comparison-20260915.json`](test/evaluation/reports/reranker-profile-comparison-20260915.json),
      with per-profile reports beside it. A prior GPU report remains in
      [`reranker-bge-m3-golden-cuda-20260914.json`](test/evaluation/reports/reranker-bge-m3-golden-cuda-20260914.json).
      The item remains open because the GTE 612 MB weights could not be
      downloaded completely and require reviewed remote code, and all results
      use the engineering-generated synthetic fixture rather than
      consent-safe release-gate judgments. The generated 133-query fixture was
      also exercised end-to-end against Qdrant and Ollama: the no-reranker
      baseline reached HitRate@5 0.789, Recall@5 0.773, MRR@10 0.641, nDCG@5
      0.665 at p50/p95 28.2/45.3ms; BGE v2 M3 on the RTX 4050 reached
      0.805/0.781/0.686/0.695 at p50/p95 165.2/253.6ms. Reports:
      [baseline](test/evaluation/reports/e2e-generated-golden-20260914.json),
      [BGE GPU](test/evaluation/reports/e2e-generated-golden-bge-cuda-20260914.json).
- [x] Add a release-gate safety mode to the reranker benchmark. It validates
      production provenance, consent, expert judgment, 100+ queries, and
      generation annotations before a report can be used for a release
      comparison; it never changes the default synthetic regression path.
- [x] Benchmark EmbeddingGemma 300M quantized against the current Nomic
      embedder on the same corpus and golden set. The isolated experiment used
      four sample documents, 133 golden queries, hybrid dense+BM25 RRF, one run
      per query, separate Qdrant collections, and a full reindex for both arms.
      Nomic used `search_query: ` / `search_document: `; EmbeddingGemma used
      its retrieval prompts `task: search result | query: ` /
      `title: none | text: `. Both produced 768-dimensional vectors and 29
      indexed points. Nomic scored HitRate@5 0.789, Recall@5 0.773, MRR@10
      0.637, nDCG@5 0.662 at p50/p95 26.9/49.2ms. EmbeddingGemma scored
      0.932/0.919/0.735/0.776 at 97.8/117.7ms, with a slightly higher
      distractor rate (0.263 vs 0.256). Ollama reported 595,142,656 bytes of
      model VRAM for Nomic versus 397,544,448 for EmbeddingGemma; Qdrant
      collection storage was effectively unchanged at 1,007,828,377 versus
      1,007,828,464 bytes. The full evidence summary is in
      [`test/evaluation/reports/embedder-comparison-20260914.json`](test/evaluation/reports/embedder-comparison-20260914.json), with raw reports in
      [`embedder-nomic-20260914.json`](test/evaluation/reports/embedder-nomic-20260914.json)
      and [`embedder-embeddinggemma-20260914.json`](test/evaluation/reports/embedder-embeddinggemma-20260914.json).
      This is an engineering-generated synthetic regression result, not
      consented production evidence; the default remains Nomic pending a
      release-gated judged corpus and representative hardware run.
- [x] Tune and calibrate dense/BM25/RRF fusion with offline golden-set
      evaluation, query-type thresholds, exact-match/header boosts, and
      deterministic score handling. The opt-in Retrieval-side path now uses
      weighted rank-RRF with provider-score isolation, deterministic key
      tie-breaking, query-type profiles, and bounded exact/header overlap
      boosts. The same 133-query fixture was evaluated against the live local
      Qdrant/Ollama stack: the provider-native baseline reached HitRate@5
      0.797, Recall@5 0.781, MRR@10 0.651, and nDCG@5 0.674, while the best
      tested local candidate (dense 2x, BM25 1x) reached MRR@10 0.647 and
      nDCG@5 0.673. No candidate improved both primary ranking metrics, so
      the default remains the low-resource provider-native RRF path and the
      calibrated path remains opt-in. Full candidate settings and reports are
      recorded in
      [`fusion-calibration-20260915.json`](test/evaluation/reports/fusion-calibration-20260915.json).
- [x] Add generation-side evaluation for faithfulness, answer relevancy, and
      context precision/recall using a judge model larger than the model under
      test. `cmd/evaluator --generation-eval` now reuses the production Chat
      prompt, requires explicit answer/judge endpoints and an operator-confirmed
      larger judge model, validates strict JSON scores, bounds each model call,
      and records per-query failures without converting them to zeroes. The
      live 133-query run used `gemma3:1b` answers and the larger
      `phi4-mini:latest` judge: 129/133 evaluated (97.0% coverage), with mean
      faithfulness 0.485, answer relevancy 0.780, context precision 0.615,
      context recall 0.622, answer p50/p95 464/1,173ms, and judge p50/p95
      1,035/1,111ms. Four cases failed (two answer timeouts and two invalid
      judge responses); they are retained in the report for follow-up instead
      of being hidden. Evidence:
      [`generation-eval-20260915.json`](test/evaluation/reports/generation-eval-20260915.json).
      The synthetic fixture remains a regression measurement, not a production
      release gate.
- [x] Move the history sidebar page size and other operational limits into
      explicit configuration if they need operational tuning. `history.session_page_size`
      and `history.turn_page_size` now control the sidebar and Qdrant turn
      scroll page; both have environment overrides and bounded validation.
- [x] Add global admission and backpressure for Retrieval fragments, reranking,
      generation, embedding, indexing, and destructive operations. The prior
      limits were per request or per process; the runtime now owns one
      process-wide admission controller with explicit finite queue timeouts
      under `admission.*`; generation holds its slot through stream completion,
      and indexing, reset, edit, and chat deletion paths reject saturated work.
      This is single-node evidence, not distributed coordination. Unit tests,
      race tests, vet, builds, and Compose validation cover the implementation.
- [x] Add domain-level metrics, traces, and structured operation IDs for
      Retrieval, Chat, Indexing, cache invalidation, and reset outcomes.
-      `internal/platform/observability` now creates request-rooted trace IDs
      and child operation IDs, records bounded outcome/duration metrics, and
      exposes `/debug/metrics`; the process-wide admission controller adds
      active/waiting/peak gauges. Retrieval, Chat (including detached stream
      supervision), Indexing, reset, and cache invalidation are instrumented.
      This is intentionally process-local telemetry; OpenTelemetry export
      remains a later deployment decision.
- [ ] Add a CI contract-drift check or OpenAPI code generation when an
      authoritative HTTP schema is introduced. The previous repository-local
      checker and unused schema snapshot were removed; the TypeScript DTO
      mirror remains the current transport seam.
- [x] Add load benchmarks for concurrent Chat streams, long-fragment Retrieval,
      and large Document ingestion with p50/p95/p99 and dependency saturation.
-      `scripts/benchmark_load.py` uses only the Python standard library, drives
      the live HTTP seams, reports p50/p95/p99, throughput, failures, and
      before/after domain and admission metrics including active, waiting,
      peak, and rejected work. Unit coverage is in
      `scripts/test_benchmark_load.py`; a live report still requires the
      operator's running Qdrant/Ollama/model topology and should be recorded
      per representative hardware profile.

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
