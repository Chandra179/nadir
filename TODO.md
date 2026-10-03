# TODO

The current finish line is a personal/local technical-notes app on the existing
laptop, with one interactive user operating the services. See
[LOCAL_V1.md](docs/LOCAL_V1.md) for the finite acceptance checklist and evidence.
Research experiments and public/multiuser deployment are outside this release.

## P1 — Finish semantic correctness before accepting the working candidate

- [ ] Preserve the selected subject/mode across follow-ups. `followup-02` still
  changes the preceding Manual choice to the Auto crash-loss warning. Diagnose
  rewrite loss and generation selection separately. Scope filtering was rejected
  because it replaced the error with an unsupported recovery guarantee.
- [ ] Make material inline claims use their actual supporting section. Latest
  failures include Secant's derivative property (`followup-01`), the expired-lock
  warning (`followup-03`), backpressure (`notes-27`) and sine at 45 degrees.
- [ ] Complete expressly requested alternatives and preserve restrictions:
  manifest formats (`notes-23`), structural/causal relations, location/AP versus
  matching/CP, and embedded-fintech delivery. Review unsupported lock conditions,
  unnamed convergence comparisons and private-IP declines in the broader pack.
- [ ] Decline conditional details absent from the source rather than infer them.
  Save the Manual crash-recovery source-coverage question for owner testing.
- [ ] Re-run both unchanged full app packs and fragile-case repetitions after a
  candidate. The bar stays 27/30 supported, 5/5 declines, 5/5 follow-ups and no new
  material regressions. A correct count or mapped citation ID is insufficient.
- [ ] Migrate an existing index only after acceptance, using a copied config and
  fresh document/cache collections with all originals available. See the
  [safe reindex steps](docs/LOCAL_V1.md#reindexing-the-working-source-policy-safely).

The latest [default-model review](test/evaluation/reports/quality-prefix-agent-review-20261002.json)
records **28/30 supported, 5/5 unsupported and 2/5 follow-ups**, versus the fresh
control's 23/30, 5/5 and 4/5. Broader results are **49/56 supported and 7/8 declines**,
versus 47/56 and 5/8. All 104 turns complete without operational failures or
unmapped IDs, but the semantic no-regression guard fails. The earlier retained
candidate's 26/30, 4/5 and 52/56 counts belong to a different arm.

The full installed-8B/CPU-embedding [comparison](test/evaluation/reports/quality-cpue-model-agent-review-20261002.json)
reviews at 26/30, 5/5, 3/5 and 49/56, 8/8. It still adds unsupported guarantees,
omits conditions and falsely declines available evidence. Keep model defaults.
CPU placement solves an observed resource problem, not these answer failures.

## Implemented and measured working changes (2026-10-02)

- [x] Fix dense task-prefix mutation leaking into lexical search/fusion, with a
  failing-then-passing regression. No reindex required for this query-only fix.
- [x] Extend existing embedding config/provider with optional `EMBEDDER_NUM_GPU`;
  verify explicit CPU zero, default omission, validation and real model coexistence.
- [x] Keep repeated rewrite details inside Inspect for the normal reading view.
- [x] Preserve bounded short-section windows, standalone bold-label scopes,
  parent heading identity and exact source anchors through indexing/cache/citations.
- [x] Qualify repeated leaf headings only under distinct Markdown parent paths;
  remove the all-ancestor policy that failed broader retrieval.
- [x] Focus hybrid English sparse queries without changing document vectors,
  literal keyword lookup, native fusion or configured models.
- [x] Present sources in retrieval order and distinguish source-local footnotes
  from application source numbering without changing the stored source.
- [x] Retain bounded prior answer context for unchanged unresolved references;
  verify named standalone queries do not inherit unrelated history.
- [x] Compare a fresh control against 133 golden and 64 broader queries, three
  runs each, and audit central text separately from expanded windows.
- [x] Review full app answers and repeat 13 fragile cases three times. Remove
  prompt/model candidates that introduce false facts or citations.

Corrected-prefix golden retrieval has Hit@5 1.000, Recall@5 0.986,
MRR@10 0.842 and nDCG@5 0.860 versus control 0.977/0.969/0.817/0.837.
Distractor hits rise 0.226 → 0.278. Broader Hit@5 remains 0.946; MRR/nDCG
rise 0.774/0.818 → 0.823/0.854. Registered retrieval bars pass; semantic
acceptance does not. Do not change reranker, fusion or model defaults from this.

Direct CPU-placement smoke completes 13/13 without operational failures. The
embedding model uses zero GPU bytes while 8B generation uses 5,277,982,720 bytes;
both remain resident. This optional setting avoids repeated embedding/answer
runner eviction on the measured laptop. Its semantic review is 9/13 and it does
not establish readiness to ship. Full CPU comparison cold daily p95 is 13.097 s;
subsequent broader p95 is 1.161 s. Do not report only the warm percentile.

## Completed local-use priorities (2026-10-01)

- [x] Save simulated general-user questions before testing, with corpus hashes,
  expected evidence, full answer/citation capture and explicit provenance.
- [x] Preserve the original follow-up question for generation; bound reference
  context separately and keep the current question after that context.
- [x] Strip prior-turn citation markers from reference answers; retain subject
  context for unchanged ordinal rewrites without contaminating standalone queries.
- [x] Decline explicit requests for an owned system's live state without turning
  sample values into observations. The English capability guard is narrow;
  it does not establish general answerability or correctness.
- [x] Check each enabled answer/rewrite model at its own endpoint during
  readiness, without loading a runner; fix model-install instructions.
- [x] Show indexed files, process-scoped last import and individual import errors;
  attach only successful imports and preserve publication/cleanup distinctions.
- [x] Fix the disabled-Docling typed-nil crash found by a real mixed upload.
- [x] Open grouped citations such as `[1, 2]` in the dashboard.
- [x] Verify duplicate skip, source replacement, cache invalidation, cancellation,
  restart history, persisted inventory, isolated reset/reindex and browser replay.
- [x] Verify the coordinated tooling batch locally, including current UI changes;
  apply the compatible brace-expansion audit patch to the current lockfile and
  save a patch for PR #15's lockfile.
- [x] Remove unsupported cache/latency claims and stale checked-in ARQMath claims.

## Administrative work

- [ ] Restore hosted Actions access and verify current status. The latest observed
  job annotation records a billing lock. A later approved read-only refresh shows
  the newest run remains September 30; current account balance is unverified. See the [saved status](test/evaluation/reports/quality-github-status-20261002.json). Jobs were not
  started; they are not evidence of test failures. Local development continues.
- [ ] Refresh and merge [PR #15](https://github.com/Chandra179/nadir/pull/15) once
  hosted checks can run. Rebase it over the brace-expansion lockfile fix; its
  original lockfile still contains that audit finding. The batch also removes
  the remaining Vitest 3 mocker advisory; do not apply `audit fix --force` ad hoc.
- [ ] Human review of the 39-case judge packet is required only before treating
  judge scores as calibrated release evidence ([issue #13](https://github.com/Chandra179/nadir/issues/13)).
  It does not block implementing or directly reviewing the local app.

## Optional follow-up work

- Validate usefulness on the owner's actual notes and saved questions when
  available. Simulated sample questions cannot establish personal usefulness.
- Conditional experiments: parent windows, distractor filtering, contextual
  enrichment, query expansion and alternative rerankers. Require measured gains.
- Optional PDF benchmark and profiling; Docling remains off for Markdown v1.
- Optional ARQMath/public-math and judge self-consistency research.
- Add an authoritative OpenAPI source when external clients actually need it.

## Future deployment requirements

Authentication, tenant isolation, shared event logs, distributed mutation
fencing, indexing leases, central telemetry and backup/restore work start only
when public or multi-instance deployment becomes an accepted goal. Current
operation gates, retention and cache epochs coordinate one API process.
