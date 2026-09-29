# TODO

This is the active engineering backlog. Completed work and historical
measurements are preserved in git history and in the committed reports under
[`test/evaluation/reports/`](test/evaluation/reports/). The condensed
evidence table lives in [`docs/OVERVIEW.md`](docs/OVERVIEW.md); decisions live
in [`docs/adr/`](docs/adr/index.md).

The backlog is ordered by risk and evidence dependency. Do not change a
Retrieval or model default from the synthetic fixture alone. Quality work must
be measured with HitRate@k, Recall@k, MRR@10, nDCG@k, answer faithfulness,
answer relevancy, context precision, and context recall. Every default change
needs a pre-registered bar and a same-session control (ADR 0005).

Items are tagged: `[bug]` correctness defect, `[testing]` missing evidence,
`[research]` needs investigation before a decision, `[approach]` possible
change to an existing method, `[method]` candidate new technique.

## Current state

The local workflow is implemented end to end: intake, chunking, embedding,
hybrid Retrieval, optional reranking, grounded Chat generation, history,
SSE replay, cancellation, reset, and process-local gates. As of 2026-09-29
([ADR 0034](docs/adr/0034-chunker-fixes-and-size.md)):

- Chunker correctness fixes are adopted (fenced code blocks indexed, list-item
  separators, oversized-part re-split, embed-input clamp). Full-corpus
  retrieval: HitRate@5 0.970–0.985, MRR@10 0.806–0.809, nDCG@5 0.831–0.840.
- Result identity includes `ChunkIndex`; multi-sentence queries also search
  the original composite query.
- The generation gate (faithfulness ≥ 0.65 **and** relevancy ≥ 0.75) passes
  for the first time: gemma3:4b 0.881/0.803 (now the default generator),
  gemma3:1b 0.750/0.756 as the low-latency fallback. `generator.num_ctx` is
  pinned; judge reports are auditable (answer + raw judge output persisted).
- Chunk size stays 512 runes: the 2048-rune arm failed both pre-registered
  ranking bars.
- The BGE v2-M3 reranker re-measured on the EmbeddingGemma default is
  net-negative on every retrieval metric at ~5× latency; it is no longer part
  of the measured quality path (see the open P1 decision below).
- Known co-existence constraint: on a 6 GiB GPU, gemma3:4b resident in
  Ollama and the CUDA reranker sidecar do not fit together; the local
  sidecar runs the CPU profile (ADR 0031 default).

Nadir is still a single-node system; gates, retention, and cache invalidation
are process-local ([ADR 0029](docs/adr/0029-ollama-scheduler-owns-llm-concurrency.md)).

## Active priority backlog

### P1 — Release confidence and core answer quality

- [ ] `[approach]` **Resolve the reranker default.** `reranker.enabled` is
      still `true` in config while the 2026-09-29 measurement shows the
      cross-encoder degrades every retrieval metric on the EmbeddingGemma
      default (HitRate@5 0.910 with vs 0.947 without; distractor@5 0.376 vs
      0.211). Either flip the default off or pre-register an adaptive-rerank
      margin calibrated from this data (the current margin threshold 0.01 was
      never validated on this embedder). Reports:
      [rerank](test/evaluation/reports/pre-fix-rerank-gpu-fullcorpus-20260929.json),
      [control](test/evaluation/reports/pre-fix-baseline-512-fullcorpus-20260929.json).
- [ ] `[approach]` **Context-selection quality.** Context precision fell to
      0.565 (gemma3:1b) / 0.480 (gemma3:4b) and `context_selection` is the
      largest diagnostic cause on the 4b arm (27/133). Evaluate: distractor
      filtering before prompt assembly, raising `top_k` with stricter
      selection, or score-threshold gating. Generation reports now persist
      per-query answers and judge output, so misses are auditable.
      [Reports](test/evaluation/reports/generation-gemma4b-fullcorpus-20260929.json).
- [ ] `[testing]` **Evaluation variance protocol.** Run-to-run HitRate@5
      varies ±0.015 on identical inputs (embedding nondeterminism and/or
      concurrent-fragment merge order). Gates should use N-run medians; if
      the source is merge nondeterminism, make fragment merge order
      deterministic. Noted in ADR 0034.
- [ ] `[testing]` **Fixture and corpus alignment.** The schema-v3 fixture
      manifest still pins the original four documents while `samples/` has
      fourteen; the manifest, the golden-corpus isolation mode, and the
      full-corpus mode should be re-based and re-recorded. Refresh annotator
      metadata before any release-gate use.
- [ ] `[testing]` **Run the ARQMath Task 1 pack through live Retrieval.** The
      importer and review tooling exist
      ([scripts/import_arqmath.py](scripts/import_arqmath.py)); the pack has
      never been evaluated against the live stack. Human review, adjudication,
      and privacy/legal approval remain pre-conditions for release-gate use.

### P2 — Correctness debt, production measurements, maintainability

- [ ] `[bug]` **Per-chunk line tracking.** `LineStart` is the section
      heading's line for every chunk in the section; citations display
      section-granular line numbers and the fixed `Key()` relies on
      `ChunkIndex` alone for within-section identity. Track real per-chunk
      line offsets through `extractSections`/`mergeSplits` (goldmark segment
      offsets are available).
- [ ] `[bug]` **Unranked keyword fallback.** `KeywordSearch` is a Qdrant
      `Scroll` with `MatchText` — no relevance ordering. Rank it (BM25 score
      or at least stable scoring) before it is used by any caller that
      matters.
- [ ] `[bug]` **Prompt budget math excludes non-context tokens.**
      `max_context_tokens` budgets context only; instructions, the question,
      and `max_output_tokens` are unaccounted, and `estimateTokens`
      (1.3×words) undercounts formula-dense text. `num_ctx` is now pinned
      (4096), so the failure mode is over-truncation, not silent clipping —
      but the estimate should become a real tokenizer count or a measured
      chars/token ratio for this corpus.
- [ ] `[testing]` **Semantic cache correctness is unmeasured.** Threshold
      0.90 cosine is plausible but nobody has measured hit-answer correctness
      vs hit rate (the GPTCache failure mode: hits ≠ correct answers). Build
      a cache-precision probe or log hit correctness in production before
      trusting it. Staleness invalidation is already versioned and tested.
- [ ] `[testing]` **Query rewriting is unmeasured.** The rewriter is enabled
      in production but absent from the evaluator path; multi-turn quality
      (pronoun resolution, query drift) has no evidence at all.
- [ ] `[testing]` **Production-topology load and PDF benchmarks.** The single
      laptop run (concurrency 8, no warmup) shows head-of-line blocking:
      `long_retrieval` p95 30 s behind 26 s chat streams. Re-run
      `make load-benchmark` on the intended topology after the reranker
      decision, plus the Docling benchmark (its 2026-09-13 report file was
      removed; only summaries survive). Capture pprof CPU/heap profiles —
      the module exists and has never been used.
- [ ] `[testing]` **gemma3:4b latency budget.** Answer p50 is 4.3 s vs 0.48 s
      for gemma3:1b. Measure streaming first-token latency and decide whether
      interactive paths should use the documented 1b fallback or whether the
      latency is acceptable everywhere.
- [ ] `[testing]` **Representative-hardware reranker comparison** (carried
      from the previous ladder). BGE v2 M3 CPU/GPU is measured on laptop
      hardware; MiniLM/GTE and production-like hardware remain open — only
      worth doing if the P1 reranker decision keeps the stage in the product.
- [ ] `[approach]` **Expose `chunk_index` in the HTTP contract.**
      `ResultResponse` carries `line_start` only; the dashboard cannot
      distinguish same-section chunks. Additive JSON field + TypeScript DTO
      + dashboard key.
- [ ] Choose the canonical HTTP contract (carried). Restore an authoritative
      OpenAPI source with CI verification if external clients appear;
      otherwise remove this item.

### P3 — Conditional experiments and new methods

- [ ] `[method]` **Small-to-big / parent-document retrieval.** The uniform
      2048 chunk failed retrieval ranking (ADR 0034), but that does not test
      the multipass mechanism: embed 512-rune chunks, feed a merged
      2048-rune parent window to the generator (Onyx-style). Targets the
      context-precision problem from P1 without touching retrieval ranking.
- [ ] `[method]` **Contextual retrieval upgrade.** The flag exists but the
      contextualizer is gemma3:1b and the corpus is clean; Anthropic reports
      −35/−49/−67% retrieval failures with a Haiku-class model. Only worth
      measuring with a stronger local contextualizer and an explicit
      ingest-cost budget.
- [ ] `[method]` **Multi-query expansion.** The original-query fragment is
      now always searched (ADR 0034); LLM-generated query variants merged by
      RRF (original weighted 2×) remain untested. Cost: +1 LLM call.
- [ ] `[method]` **Judge robustness.** Single judge sample per query;
      evaluate n-sample median/self-consistency and whether the judge
      handles terse formula answers fairly (known mis-scoring documented in
      the 2026-09-27 evidence).
- [ ] `[research]` Collect explicit relevance/user-selection labels, then
      evaluate a small learned-to-rank model over dense score, BM25 score,
      RRF rank, metadata, exact-match, and position features. Require an
      out-of-sample gain before adding the lifecycle complexity.
- [ ] `[research]` Prototype SPLADE-v3 as an optional learned-sparse leg
      behind a flag; measure inference cost, RAM, sparse index size, and
      quality against BM25.
- [ ] `[research]` Prototype ColBERT-style late interaction only when corpus
      scale or quality evidence justifies precomputed token vectors + MaxSim.
- [ ] `[research]` Answer-confidence, unsupported-answer, and filter-miss
      telemetry; evaluate Retrieval fallback or abstention against
      false-confidence rates.

## Deployment-gated work

Do not implement these for the current local single-node product. Start them
only when the corresponding operational requirement is accepted and measured.

- [ ] For horizontal Chat: shared ordered event-log Adapter, global turn
      routing, replay/fan-out tests, and a failure policy for a disappearing
      generation owner.
- [ ] For distributed Session mutations: shared conditional writes or fencing
      tokens instead of process-local revisions.
- [ ] For distributed Indexing: shared source storage/manifest, leases,
      generation-aware commits, deletion ownership, recoverable jobs.
- [ ] Before untrusted users: authentication, authorization, CSRF protection
      where applicable, audit logging, rate limits, backup/restore drills,
      tenant isolation.
- [ ] For multi-instance deployments: centralized OpenTelemetry-compatible
      traces/metrics, dependency saturation metrics, and alerts.

## Rejected for the current domain

- HyPE: removed from the codebase; ingest-time LLM cost per chunk was not
  justified by measured gain.
- Semantic chunking: 2024–2026 benchmarks (NAACL 2025; FloTorch 2026) show no
  consistent gain over recursive splitting; not pursued here.
- Uniform chunk_size 2048 runes: failed both pre-registered ranking bars
  (ADR 0034). Do not revisit without new corpus evidence.
- Late chunking: requires token-level long-context embedding (incompatible
  with the Ollama-hosted EmbeddingGemma path) and has not justified its
  tradeoffs here.
- HyDE and GraphRAG/RAPTOR: excessive machinery or brittleness for the
  current precise numeric/entity workload and corpus size.
