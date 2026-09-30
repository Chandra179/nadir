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
SSE replay, cancellation, reset, and process-local gates. As of 2026-09-30
(on top of [ADR 0034](docs/adr/0034-chunker-fixes-and-size.md)):

- Startup defaults are consistent everywhere: `config.yaml`, Compose, and
  `.env.example` all use EmbeddingGemma-300m with its task prefixes and the
  gemma3:4b generator, guarded by a deployment-consistency test
  (`internal/bootstrap/configuration/deployment_test.go`). Reranking is
  opt-in (`reranker.enabled: false` everywhere); the reranker sidecar moved
  behind the Compose `rerank` profile, no longer gates API startup, and
  `scripts/local.sh` starts it only when enabled (`--startup-config` prints
  the effective local settings). Changing the embedding space requires
  reset-and-reindex.
- The evaluator was repaired (report schema v2): quality is the median of
  dataset-level metrics across runs while latency pools every request;
  retrieval depth is at least 10 with an explicit MRR@10 cutoff; nDCG is
  graded and identity-aware with duplicate evidence collapsed (no more >1.0
  scores); unsupported/abstention queries are first-class with an
  `abstention_score`; reports carry per-run distributions and full
  provenance — golden-set SHA, corpus manifest verification, effective-config
  hash, and model metadata observed from the serving endpoint. The judge
  prompt scores terse complete answers fairly and requires an explicit
  suitability acknowledgement plus a judge model distinct from the answer
  model (by name and observed digest).
- A representative evaluation pack covers all 14 sample documents:
  `test/evaluation/representative.json` (64 queries, including 8 genuine
  unsupported queries, with a content-hashed corpus manifest). The original
  133-query math pack is preserved unchanged as the fixed regression set.
- Publication and cache freshness are failure-safe at the Document seam:
  `indexing.PublicationError` distinguishes a visible mutation with
  unfinished cleanup from an unpublished failure; replacement retries never
  restage over an activated version (`publicationMu` plus a visible-version
  check); the semantic cache suspends reuse during any publication, binds
  writes to the generation observed before retrieval, and starts each
  process on a fresh cache epoch; reset invalidates the cache even when
  cleanup fails. Fault-injection tests cover replace, reset, and
  mirror-removal paths (`publication_guards_test.go`,
  `replacement_retry_test.go`, `concurrency_test.go`,
  `cache_freshness_test.go`).
- Prompt construction selects evidence by retrieval rank before edge
  arrangement (a lower-ranked chunk can no longer steal budget from a
  higher-ranked one), carries a citation map — number, retrieval rank, path,
  header, line, chunk index — through history and the HTTP contract to the
  dashboard, and budgets the complete model request (instructions, question,
  reserved output, template allowance) against the pinned `num_ctx` with a
  conservative estimator. Chunker output now carries real per-chunk source
  lines (span-based mapping through extraction, splitting, and overlap).
- The generation gate (faithfulness ≥ 0.65 **and** relevancy ≥ 0.75) passed
  for the first time with the pre-repair judge: gemma3:4b 0.881/0.803 (now
  the default generator), gemma3:1b 0.750/0.756 as the low-latency fallback.
  Chunk size stays 512 runes: the 2048-rune arm failed both pre-registered
  ranking bars.
- Known co-existence constraint: on a 6 GiB GPU, gemma3:4b resident in
  Ollama and a CUDA reranker sidecar do not fit together; the local sidecar
  runs the CPU profile when opted in (ADR 0031 default).

Nadir is still a single-node system; gates, retention, and cache invalidation
are process-local ([ADR 0029](docs/adr/0029-ollama-scheduler-owns-llm-concurrency.md)).

## Active priority backlog

### P1 — Release confidence and measurement credibility

- [ ] `[testing]` **Re-record live evidence with the repaired evaluator on
      the corrected defaults.** Every committed report predates both the
      evaluator repairs and the reranker opt-in, so no committed number is
      comparable across that boundary. Run the retrieval arm (`--runs >= 3`)
      on `golden.json` and `representative.json` with and without the
      reranker, plus the generation arm (gemma3:4b, independent judge) with
      abstention coverage; commit the reports and update
      [`docs/OVERVIEW.md`](docs/OVERVIEW.md). Re-confirm the reranker opt-in
      decision from the new control pair — re-enabling it requires a measured
      net gain on the current embedding profile.
- [ ] `[testing]` **Calibrate the judge against human judgments.** The judge
      prompt now handles terse answers and abstention, but its calibration
      status stays `unreviewed` until a human pass lands. Verify the
      deployed judge's serving fingerprint (observed parameter count, not
      the model tag), then run
      [scripts/calibrate_evaluation_judge.py](scripts/calibrate_evaluation_judge.py):
      blind human scoring of the persisted answer/context/judge bundles,
      agreement analysis, and prompt or threshold adjustment before any
      release-gate use. Refresh annotator metadata on the golden pack at the
      same time.
- [ ] `[testing]` **Run the ARQMath Task 1 pack through live Retrieval.** The
      importer and review tooling exist
      ([scripts/import_arqmath.py](scripts/import_arqmath.py)); the pack has
      never been evaluated against the live stack. Human review, adjudication,
      and privacy/legal approval remain pre-conditions for release-gate use.

### P2 — Correctness debt, production measurements, maintainability

- [ ] `[testing]` **Measure the enabled user paths on the corrected
      defaults.** Semantic-cache hit correctness vs the 0.90 threshold and
      multi-turn rewriting quality now have tooling
      ([scripts/benchmark_user_paths.py](scripts/benchmark_user_paths.py),
      `make user-path-benchmark`, `test/evaluation/user-paths.json`) but no
      live evidence yet. Add streaming first-token latency for gemma3:4b vs
      the documented 1b fallback, and a concurrent `make load-benchmark` run
      on the current topology — the older report's retrieval p95 near 30 s
      is unexplained under the current defaults.
- [ ] `[approach]` **Context-selection quality.** Context precision fell to
      0.565 (gemma3:1b) / 0.480 (gemma3:4b) and `context_selection` is the
      largest diagnostic cause on the 4b arm (27/133). The persisted
      admitted-context and citation map make misses auditable end to end.
      Evaluate: distractor filtering before prompt assembly, raising `top_k`
      with stricter selection, or score-threshold gating.
      [Reports](test/evaluation/reports/generation-gemma4b-fullcorpus-20260929.json).
- [ ] `[bug]` **Unranked keyword fallback.** `KeywordSearch` is a Qdrant
      `Scroll` with `MatchText` — no relevance ordering. Rank it (BM25 score
      or at least stable scoring) before it is used by any caller that
      matters.
- [ ] `[testing]` **Production-topology load, Docling benchmark, and pprof.**
      Re-run `make load-benchmark` on the intended topology, re-record the
      Docling benchmark (its 2026-09-13 report file was removed; only
      summaries survive), and capture pprof CPU/heap profiles — the module
      exists and has never been used.
- [ ] `[approach]` **Judge n-sample self-consistency.** One judge sample per
      query remains; evaluate an n-sample median once the human calibration
      above lands.
- [ ] `[testing]` **Dependency updates.** Merge the routine patches first
      with CI green (typescript-eslint 8.70.1, golang 1.27.1-alpine, alpine
      3.24); land the four major frontend tooling upgrades (vite 8,
      @vitejs/plugin-react 6, vitest 5, jest-dom 7) as one coordinated
      upgrade after the current changeset merges, gated on the dashboard
      test suite.
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
- [ ] `[testing]` **Representative-hardware reranker comparison.** Only worth
      doing if the reranker returns to the product path (it is opt-in
      today): BGE v2 M3 CPU/GPU was measured on laptop hardware;
      MiniLM/GTE and production-like hardware remain open.
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
