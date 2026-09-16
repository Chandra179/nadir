# TODO

This is the active engineering backlog. Completed work and historical
measurements are preserved in
[`docs/roadmap/archive.md`](docs/roadmap/archive.md).

The backlog is ordered by risk and evidence dependency. Do not change a
Retrieval or model default from the synthetic fixture alone. Quality work must
be measured with HitRate@k, Recall@k, MRR@10, nDCG@k, answer faithfulness,
answer relevancy, context precision, and context recall.

## Current state

Nadir's core local workflow is implemented: Document intake, Indexing,
hybrid Retrieval, optional reranking, grounded Chat generation, history
editing/deletion, SSE replay, cancellation, reset, bounded retention, and
process-local admission control. Go race tests, vet, builds, Python benchmark
tests, and dashboard checks currently pass.

Nadir is still a single-node system. Chat event retention, Session mutation
ordering, Indexing ownership, cache invalidation, and admission budgets are
process-local. See [`docs/SCALING.md`](docs/SCALING.md) before planning
horizontal deployment.

## Current evidence

The active 133-query fixture covers the four sample Documents and is an
expert-authored synthetic regression fixture. It is not consent-safe
production evidence and must not be used alone for a release decision.

| Scope | Evidence | Meaning |
|---|---|---|
| Schema-v3 synthetic candidate pack | 133 queries, four-document manifest, two recorded synthetic judgment passes, per-query adjudication, `release_gate=false` | Contract and regression evidence only; annotators are not independent humans |
| ARQMath public review-pack candidate | 120 public Task 1 queries, 40 each from the 2020–2022 editions, pinned topic/qrels hashes, fixed qrel candidate pools, `release_gate=false` ([pack](test/evaluation/arqmath/arqmath-review-pack.json), [report](test/evaluation/arqmath/arqmath-review-report.json)) | Reproducible acquisition/review input; the full Posts corpus, human reviews, adjudication, privacy/legal approval, and live Retrieval evaluation are still pending |
| Hybrid Retrieval without reranking | MRR@10 0.651, nDCG@5 0.674, p50/p95 22/47ms ([report](test/evaluation/reports/fusion-baseline-20260915.json)) | Fast baseline; provider-native RRF remains the fusion default |
| Live E2E regression run (133-query candidate, no reranker) | HitRate@5 0.797, Recall@5 0.781, MRR@10 0.651, nDCG@5 0.671, p50/p95 25.9/43.5ms; 0 evaluator errors ([report](test/evaluation/reports/e2e-synthetic-candidate-20260916.json)) | Qdrant/Ollama path verified locally; synthetic evidence only |
| End-to-end BGE GPU Retrieval | MRR@10 0.686, nDCG@5 0.695, p50/p95 165/254ms ([report](test/evaluation/reports/e2e-generated-golden-bge-cuda-20260914.json)) | Product-path evidence, still synthetic |
| Generation judge baseline | Faithfulness 0.485, relevancy 0.780, context precision/recall 0.615/0.622; 129/133 evaluated ([report](test/evaluation/reports/generation-eval-20260915.json)) | Answer quality is not release-ready evidence; captured triage is in [generation-triage-20260916.json](test/evaluation/reports/generation-triage-20260916.json) |
| Reranker profiles | BGE CPU p50/p95 561/2,927ms; MiniLM int8 ONNX 11/75ms; GTE unavailable ([report](test/evaluation/reports/reranker-profile-comparison-20260915.json)) | Hardware comparison is incomplete and isolated from end-to-end answer quality |
| PDF intake | 18/18 successful; p50/p95 2.62/25.94s; peak RSS about 3.28GiB ([report](test/evaluation/reports/docling-system-corpus-20260913.json)) | Local-process baseline, not container-capacity evidence |

The current configuration enables BGE v2 M3 CPU reranking by default. That is
an operational choice, not a release-validated model decision; its tail
latency must be resolved before calling the default production-ready.

## Active priority backlog

### P0 — Data correctness and destructive-operation safety

No open P0 items. Completed P0 work is preserved in
[`docs/roadmap/archive.md`](docs/roadmap/archive.md).

### P1 — Release confidence and core answer quality

These items come before model or architecture experiments.

- [x] Create a consent-safe, expert-authored evaluation fixture with at least
      100 user-intent queries over the committed sample corpus. The active
      schema-v3 candidate contains 133 queries with expected answers, required
      claims, canonical relevant passages, two synthetic judgment passes,
      per-query adjudication, query-intent tags, and a corpus manifest. It has
      been exercised through the
      live end-to-end evaluator ([report](test/evaluation/reports/e2e-synthetic-candidate-20260916.json)).
      It remains regression/dry-run evidence.
- [x] Add reproducible public ARQMath Task 1 acquisition and review-pack
      tooling. The checked-in candidate selects 40 queries from each 2020,
      2021, and 2022 edition, verifies the six pinned topic/qrels artifacts,
      preserves fixed candidate pools, and keeps `release_gate=false`. The
      importer can verify and normalize the full Posts snapshot when a trusted
      checksum is supplied. It does not fabricate human review or approval.
- [ ] Complete the generation-quality remediation gate. The implementation is
      complete: per-query paired Retrieval/context diagnostics, section-header
      matching and prompt context, bounded deterministic answer output,
      structured bounded judge output, strict score validation, and regression
      cases are in place ([triage report](test/evaluation/reports/generation-triage-20260916.json)).
      Remaining evidence is a live 133-query rerun with Qdrant/Ollama: require
      zero judge-contract failures, no repeated cosine timeout, complete
      classifications, and no Retrieval or answer-quality regression before
      checking this item.
- [ ] Make the default Retrieval profile an explicit release decision. Compare
      no reranker, fast quantized CPU reranking, BGE CPU, and BGE GPU against
      the agreed quality and latency criteria; document which deployment
      profile each default supports.

### P2 — Production measurements and maintainability

- [ ] Complete the representative-hardware reranker comparison after the P1
      evaluation input and acceptance criteria exist. Compare only the models
      required by the product, including BGE v2 M3 CPU/GPU, MiniLM L6 and its
      quantized ONNX path, and GTE multilingual reranker base if multilingual
      queries are in scope. Record quality, p50/p95 latency, RAM, VRAM,
      startup, throughput, failures, and timeouts. The existing synthetic
      profiles are engineering evidence only:
      [`reranker-profile-comparison-20260915.json`](test/evaluation/reports/reranker-profile-comparison-20260915.json).
- [ ] Run the load benchmark and PDF benchmark against the intended
      production-like CPU/GPU topology, then record p50/p95/p99 latency,
      throughput, dependency saturation, memory, failures, and timeouts. The
      existing harnesses and local reports do not prove target capacity.
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
- [ ] Revisit Qdrant quantization or an embedder replacement only when corpus
      size, memory, latency, or golden-set quality demonstrates a real ceiling.
      Any change requires a full reindex and an isolated comparison.
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
