# TODO

This is the active engineering backlog. Completed work and historical
measurements are preserved in git history and in the committed reports under
[`test/evaluation/reports/`](test/evaluation/reports/).

The backlog is ordered by risk and evidence dependency. Do not change a
Retrieval or model default from the synthetic fixture alone. Quality work must
be measured with HitRate@k, Recall@k, MRR@10, nDCG@k, answer faithfulness,
answer relevancy, context precision, and context recall.

## Current state

Nadir's core local workflow is implemented: Document intake, Indexing,
hybrid Retrieval, optional reranking, grounded Chat generation, history
editing/deletion, SSE replay, cancellation, reset, bounded retention, and
process-local operation gates. Go race tests, vet, builds, Python benchmark
tests, and dashboard checks currently pass. The stack runs end to end under
rootless Podman, and the reranker sidecar is validated on both CPU and an
NVIDIA GPU via the CDI Compose overlay.

Nadir is still a single-node system. Chat event retention, Session mutation
ordering, Indexing ownership, cache invalidation, and gate budgets are
process-local. See [`AGENTS.md`](AGENTS.md) and
[`docs/adr/0029-ollama-scheduler-owns-llm-concurrency.md`](docs/adr/0029-ollama-scheduler-owns-llm-concurrency.md)
before planning horizontal deployment.

## Current evidence

The active 133-query fixture is an expert-authored synthetic regression
fixture over the sample corpus (expanded to 14 documents after the fixture's
corpus manifest was captured). It is not consent-safe production evidence and
must not be used alone for a release decision.

| Scope | Evidence | Meaning |
|---|---|---|
| Schema-v3 synthetic candidate pack | 133 queries, two recorded synthetic judgment passes, per-query adjudication, `release_gate=false` ([fixture](test/evaluation/golden.json)) | Contract and regression evidence only; annotators are not independent humans |
| ARQMath public Task 1 acquisition tooling | Importer selects 40 queries per 2020–2022 edition with pinned, SHA-256-verified topic/qrels artifacts and dual-review merge to schema-v3 ([importer](scripts/import_arqmath.py)); the regenerable review pack is not committed | Reproducible acquisition/review input; the full Posts corpus, human reviews, adjudication, privacy/legal approval, and live Retrieval evaluation are still pending |
| Hybrid Retrieval without reranking | HitRate@5 0.805, Recall@5 0.788, MRR@10 0.657, nDCG@5 0.678, p50/p95 19/23ms ([report](test/evaluation/reports/e2e-podman-no-rerank-goldencorpus-20260926.json)) | Fast baseline; provider-native RRF remains the fusion default |
| End-to-end BGE v2-M3 reranking on CPU | HitRate@5 0.812, Recall@5 0.794, MRR@10 0.697, nDCG@5 0.709; rerank p50/p95 9.36/18.6s ([report](test/evaluation/reports/e2e-podman-rerank-20260926.json)) | Best measured quality; CPU rerank tail latency is the operational blocker for default-on |
| End-to-end BGE v2-M3 reranking on GPU (laptop RTX, CDI overlay) | Quality identical (MRR@10 0.697, nDCG@5 0.709); rerank p50/p95 435/700ms; peak VRAM ≈3.7 of 6.1 GiB ([report](test/evaluation/reports/e2e-podman-gpu-rerank-20260927.json)) | ≈21× faster reranking than CPU with no quality change; laptop hardware so far |
| End-to-end BGE v2-M3 reranking, quantized int8 CPU (2026-09-27, full 14-document corpus) | HitRate@5 0.820, Recall@5 0.796, MRR@10 0.725, nDCG@5 0.731; rerank p50/p95 5.42/9.70s; serving profile recorded in-report (backend onnx-int8, device cpu) ([report](test/evaluation/reports/e2e-podman-rerank-onnx-int8-20260927.json)) | Quality at or above torch, but only ≈1.7× faster — fails the pre-registered rerank-p50 ≤ 2s bar; the CPU default stays torch ([ADR 0031](docs/adr/0031-default-retrieval-profile-torch-cpu.md)) |
| Load, chat streams at concurrency 8 | p50/p95/p99 26.0/53.2/55.2s, 0 failures ([report](test/evaluation/reports/load-podman-20260926.json)) | Single-process, LLM-bound laptop baseline; not target-capacity evidence |
| Generation judge baseline (pre-remediation) | Faithfulness 0.485, relevancy 0.780, context precision/recall 0.615/0.622; 129/133 evaluated ([triage](test/evaluation/reports/generation-triage-20260916.json)) | Pre-remediation numbers; the live rerun below is the same fixture re-measured after the remediation |
| Generation judge, post-remediation live rerun (2026-09-27, 4-document golden-corpus isolation) | Faithfulness **0.716**, relevancy 0.662, context precision/recall 0.663/0.711; 133/133 evaluated, 0 failures ([report](test/evaluation/reports/e2e-podman-generation-rerun-20260927.json)) | All mechanical gate criteria pass. The relevancy aggregate dropped 0.780→0.662, but the paired per-query triage locates it in the 26 retrieval-miss queries (relevancy 0.623→0.335 where faithfulness rose 0.181→0.615 — the bounded style now refuses instead of hallucinating) and in the judge mis-scoring terse formula answers (e.g. a 19-byte correct answer scored faithfulness 0.0 and context recall 0.0 against context precision 1.0). Retrieval-hit queries: relevancy 0.789→0.742 (noise-level), faithfulness 0.541→0.741 |
| Judge-instrument fix, same answers re-judged (2026-09-27) | Faithfulness 0.729, relevancy 0.682; 132/133 evaluated, 1 judge-contract failure ([report](test/evaluation/reports/e2e-podman-generation-rerun-judgefix-20260927.json)) | The corrected judge (terse-but-complete answers score high; brevity is not unfaithfulness) explains only ≈+0.02 of the relevancy gap — the ruler was a minor factor; the answers were genuinely less relevant |
| Answer-prompt recalibration: question-proportional shaping + informative abstention (2026-09-27) | Faithfulness **0.690**, relevancy **0.733**, context precision/recall **0.689/0.736**; 133/133 evaluated, 0 failures ([report](test/evaluation/reports/e2e-podman-generation-promptfix-20260927.json)) | Largest single-lever gain (relevancy +0.071) with faithfulness held far above the 0.485 baseline; still below the pre-registered 0.75 relevancy bar by 0.017 — the residual tracks the 26 retrieval-miss queries; the retrieval-side priority ladder below is the path to closing it |
| Fusion A/B: weighted rank-RRF vs Qdrant-native RRF (2026-09-27, both corpus states, same-session controls) | Fusion loses on both: full corpus HitRate@5 0.744 / MRR@10 0.602 vs control 0.759/0.641; golden 0.789/0.636 vs 0.797/0.641 ([reports](test/evaluation/reports/e2e-podman-fusion-on-goldencorpus-20260927.json)) | The shipped default fusion profile (equal weights, k=60, no boosts) does not beat native RRF anywhere; the flag stays opt-in and off |
| Fresh EmbeddingGemma reindex (2026-09-27, current build, full reindex both arms) | Golden: HitRate@5 **0.955**, Recall@5 0.941, MRR@10 0.742, nDCG@5 0.787 at embed p50/p95 95/106ms; full 14-doc corpus: **0.940**, 0.929, **0.795**, 0.823. Same-session Nomic controls: 0.797 golden / 0.759 full ([reports](test/evaluation/reports/embedder-gemma-goldencorpus-20260927.json)) | The 2026-09-14 experiment reproduces and improves on the current build; converts roughly 21 of the 26 retrieval-miss queries at 3-4x embedding latency |
| Generation-gate attempts on the swapped embedder (2026-09-27) | Gemma no-rerank: faithfulness 0.632 / relevancy **0.757**; with reranker: 0.663 / 0.689 ([reports](test/evaluation/reports/generation-gemma-promptfix-20260927.json)) | Each configuration passes exactly one bar; the three runs bracket the pre-registered gate (see the P1 item) — the binding constraint is now the answer model, not retrieval |
| PDF intake | 18/18 conversions, p50/p95 2.62/25.94s, peak RSS ≈3.28 GiB (measured 2026-09-13; earlier report removed in the stale-evidence cleanup) | Local-process baseline, not container-capacity evidence |

The current configuration enables BGE v2 M3 CPU reranking by default. That
default is now a measured release decision ([ADR 0031](docs/adr/0031-default-retrieval-profile-torch-cpu.md)):
the pre-registered comparison kept torch CPU for portable operation — the
quantized int8 route matched quality (MRR@10 0.725 vs 0.697) but rerank p50
5.42s missed the ≤ 2s bar — and the GPU Compose overlay is the documented
profile for latency-sensitive deployments (identical quality, rerank p50
435ms). Any revisit of int8 serving tuning requires a new pre-registered
measurement.

## Active priority backlog

### P0 — Data correctness and destructive-operation safety

No open P0 items. Completed P0 work is preserved in git history and in the
committed evaluation reports.

### P1 — Release confidence and core answer quality

These items come before model or architecture experiments.

- [x] Create a consent-safe, expert-authored evaluation fixture with at least
      100 user-intent queries over the committed sample corpus. The active
      schema-v3 candidate contains 133 queries with expected answers, required
      claims, canonical relevant passages, two synthetic judgment passes,
      per-query adjudication, query-intent tags, and a corpus manifest. It is
      exercised through the live end-to-end evaluator; the latest full-corpus
      run is the
      no-rerank Podman report ([report](test/evaluation/reports/e2e-podman-no-rerank-goldencorpus-20260926.json)).
      It remains regression/dry-run evidence.
- [x] Add reproducible public ARQMath Task 1 acquisition and review-pack
      tooling. The importer selects 40 queries from each 2020, 2021, and 2022
      edition, verifies the six pinned topic/qrels artifacts, preserves fixed
      candidate pools, and keeps `release_gate=false`. The generated pack is
      regenerable rather than committed. The importer can verify and normalize
      the full Posts snapshot when a trusted checksum is supplied. It does not
      fabricate human review or approval.
- [ ] Complete the generation-quality remediation gate. The implementation is
      complete and three live 133-query runs have isolated the levers
      (see the evidence table): the judge-instrument fix contributed ≈+0.02
      relevancy, and the answer-prompt recalibration (question-proportional
      answer shape plus informative abstention) contributed +0.071, reaching
      faithfulness 0.690 / relevancy 0.733 with zero judge-contract failures
      and no retrieval regression. The pre-registered gate bar
      (faithfulness ≥ 0.65, relevancy ≥ 0.75) is met on faithfulness and
      missed on relevancy by 0.017; the residual gap tracks the 26
      retrieval-miss queries. HyPE was removed from the codebase (its
      ingest-time LLM cost per chunk was not justified). The retrieval-side
      ladder has since been measured (see the evidence table): the fusion
      profile loses to native RRF on both corpus states and stays off, and
      the EmbeddingGemma reindex converts most retrieval misses
      (HitRate@5 0.955 golden / 0.940 full vs 0.797/0.759 Nomic controls).
      The default embedder is now EmbeddingGemma (ADR 0032). Three gate
      attempts on it each pass exactly one pre-registered bar — Run B 0.690/0.733, Gemma 0.632/0.757, Gemma plus
      reranker 0.663/0.689 — so the gate stays open on evidence, not on
      effort: faithfulness and relevancy trade against each other through
      retrieval richness and answer verbosity, and the binding constraint is
      now the answer model (gemma3:1b). The remaining levers are a larger
      generator model (latency and quality tradeoffs to measure) or an
      explicit, documented revision of the gate bar; no further
      configuration permutations of the current stack are planned.
- [x] Make the default Retrieval profile an explicit release decision. All
      four profiles are measured against the same fixture and the decision is
      recorded in [ADR 0031](docs/adr/0031-default-retrieval-profile-torch-cpu.md):
      torch fp32 CPU stays the portable default — the quantized int8 route
      matched quality (MRR@10 0.725 vs 0.697) but rerank p50 5.42s failed the
      pre-registered ≤ 2s bar — and the GPU Compose overlay covers
      latency-sensitive deployments (identical quality, rerank p50 435ms).

### P2 — Production measurements and maintainability

- [ ] Complete the representative-hardware reranker comparison after the P1
      evaluation input and acceptance criteria exist. Compare only the models
      required by the product, including BGE v2 M3 CPU/GPU, MiniLM L6 and its
      quantized ONNX path, and GTE multilingual reranker base if multilingual
      queries are in scope. Record quality, p50/p95 latency, RAM, VRAM,
      startup, throughput, failures, and timeouts. Progress: BGE v2 M3
      CPU-vs-GPU is measured on a laptop RTX (see the evidence table); older
      multi-profile synthetic tables were removed with the stale reports and
      live in git history. Production-like hardware is still pending.
- [ ] Run the load benchmark and PDF benchmark against the intended
      production-like CPU/GPU topology, then record p50/p95/p99 latency,
      throughput, dependency saturation, memory, failures, and timeouts. The
      existing harnesses and the laptop-topology load report do not prove
      target capacity.
- [ ] Benchmark recursive versus sentence-window chunking on the release-gated
      corpus before changing the chunker default. Include retrieval and answer
      quality, chunk count, index size, ingest cost, and query latency.
- [ ] Choose the canonical HTTP contract. If external clients or independent
      teams need a stable contract, restore an authoritative OpenAPI source and
      generate or verify the TypeScript DTO mirror in CI. If the dashboard
      remains the only client, keep the current transport tests and remove this
      item rather than maintaining an unused schema.

### P3 — Conditional Retrieval and learning experiments

- [ ] Add answer-confidence, unsupported-answer, and filter-miss telemetry;
      then evaluate Retrieval fallback or abstention against false-confidence
      and noise rates. Existing adaptive reranking only decides whether to
      call the reranker.
- [x] Revisit Qdrant quantization or an embedder replacement only when corpus
      size, memory, latency, or golden-set quality demonstrates a real ceiling.
      Any change requires a full reindex and an isolated comparison. Done: the
      golden-set ceiling was demonstrated (HitRate@5 0.797 vs 0.955), two
      isolated full-reindex comparisons ran (2026-09-14 and 2026-09-27), and
      the embedder replacement is adopted as the default
      ([ADR 0032](docs/adr/0032-default-embedder-embeddinggemma.md)). Qdrant
      quantization remains open only if scale or memory evidence appears.
- [ ] Collect explicit relevance and user-selection labels, then evaluate a
      small learned-to-rank model over dense score, BM25 score, RRF rank,
      metadata, exact-match, and position features. Require an out-of-sample
      gain before adding training and serving lifecycle complexity.
- [ ] Prototype SPLADE-v3 only as an optional learned-sparse leg behind a
      feature flag. Measure inference cost, RAM, sparse index size,
      nonzero-term counts, and quality against BM25; it is not a lightweight
      replacement on the current laptop.
- [ ] Prototype ColBERT-style late interaction only after corpus scale or
      quality evidence justifies precomputed token vectors and MaxSim. Measure
      index growth, ingest throughput, RAM, query latency, and quality before
      considering it as a Retrieval or reranking replacement.

## Deployment-gated work

Do not implement these for the current local single-node product. Start them
only when the corresponding operational requirement is accepted and measured.

- [ ] For horizontal Chat, add a shared ordered event-log Adapter, global turn
      routing, replay/fan-out tests, and a failure policy for a generation owner
      disappearing.
- [ ] For distributed Session mutations, replace process-local revisions and
      sequence allocation with shared conditional writes or fencing tokens.
- [ ] For distributed Indexing, add shared source storage/manifest, leases,
      generation-aware commits, deletion ownership, and recoverable jobs.
- [ ] Before exposing the system to untrusted users, add authentication,
      authorization, CSRF protection where applicable, audit logging, rate
      limits, backups/restore drills, and tenant isolation.
- [ ] When multiple instances or model pools are deployed, add centralized
      OpenTelemetry-compatible traces/metrics, dependency saturation metrics,
      and alerts. The current `/debug/metrics` endpoint is intentionally
      process-local.

## Research candidates — deliberately deferred

CRAG, speculative RAG, Adaptive-RAG routing, GraphRAG, RAPTOR, and multi-query
expansion remain research candidates. They should not precede the P1 quality
work or the P2 chunking/model measurements.

## Rejected for the current domain

- HyDE and multi-query expansion: unnecessary for the current precise
  numeric/entity workload; fragment splitting already captures part of the
  benefit.
- GraphRAG and RAPTOR: excessive machinery for the current corpus size.
- Late chunking: requires long-context pooling and has not justified its
  relevance/completeness tradeoff here.
