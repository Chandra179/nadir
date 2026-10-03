# Personal/local v1

The goal is a dependable Markdown technical-notes reading companion for one
person on the current laptop. The workflow is import, ask, inspect evidence,
follow up, cancel and return after restart. The user operates Qdrant, Ollama and
the launcher/dashboard. Packaging, PDFs, public hosting and multiuser operation
are outside this finish line.

## Current acceptance (2026-10-03)

The finite local engineering checklist passes with the installed defaults.
No additional model was downloaded for the accepted fixes. Generator is
`gemma3:4b`, rewriter `gemma3:1b`, embedder `embeddinggemma-300m-q8:latest`;
reranking and custom fusion remain disabled. The earlier authorized Qwen
comparison and installed 8B experiment were not adopted.

| Area | Required outcome | Evidence |
|---|---|---|
| Setup | Configured role models are checked at their own endpoints; missing enabled models prevent readiness | Current role tests, live readiness and serving provenance |
| Import | Visible successful sources, unchanged-file skipping, individual failure reasons, disabled PDF safety | Current regression tests; 2026-10-01 mixed import; current migration imports 15 files without failures and skips 14 on re-import |
| Source updates | New versions replace old evidence and invalidate cache | Current indexing/cache regressions and dated 2026-10-01 live replacement/cache canary |
| Chat lifecycle | Cancel persists partial answer; restart preserves history/inventory; browser replay and citations work | Current chat tests; 2026-10-03 persisted Subject/inventory restart; 2026-10-01 API/browser lifecycle and 2026-10-02 browser checks |
| Supported questions | At least 27/30 useful supported answers | **30/30**, actual question and cited-evidence review |
| Unsupported questions | 5/5 clear declines without substituting sample values | **5/5** |
| Follow-ups | 5/5 preserve intended subject and substantiate the answer or limitation | **5/5**; Manual remains selected and its undocumented crash outcome is declined |

These are fixed simulated questions authored before testing, not owner queries,
independent human review or calibrated automated-judge scores. Review answers
what the actual question asks; fixture lists can include unasked extra facts.
The complete [fitness report](p1-evidence.md) labels current and historical
checks and records the limits of these results.

## Current answer and retrieval evidence

| App pack | Supported | Declines | Follow-ups |
|---|---:|---:|---:|
| Daily, 40 questions | 30/30 | 5/5 | 5/5 |
| Broader, 64 questions | 56/56 | 8/8 | — |
| Pre-selected fragile cases, three runs | 36/36 | — | 3/3 |

All 143 turns complete with no operational failures or unmapped citation IDs.
The same 104 full-pack questions also pass direct review on the migrated
15-note corpus. That migration run is a separate corpus check, not the controlled
14-sample comparison.

No new material failures were found against the **saved fresh 2026-10-02
control**: daily 23/30, 5/5, 4/5; broader 47/56, 5/8. The control was not recreated
on October 3. Corpus and query hashes are unchanged in the controlled runs;
model identities and serving source/binary hashes are recorded. An extra mapped
source number alone never establishes semantic support.

The unchanged retrieval packs run three times each (591 requests):

| Metric | Golden, 133 queries | Broader, 64 queries |
|---|---:|---:|
| Hit@5 | 1.0000 | 0.9464 |
| Recall@5 | 0.9825 | 0.9464 |
| MRR@10 | 0.8504 | 0.8229 |
| nDCG@5 | 0.8640 | 0.8544 |
| Distractor hit@5 | 0.2707 | 0.2344 |

Hit/MRR/nDCG meet the registered maximum 0.01 regression limit. Fresh-index
rank ties can differ; these measured values are not copied from the older run.
The prior broader-control distractor rate was 0.2188, so exposure remains higher.
No reranker, fusion or model default was changed to obtain these results.
Daily first-token p50/p95 is 0.713/0.981 seconds; broader 0.595/0.756 seconds,
excluding immediate answers. These are sequential app replay observations, not
cold-load guarantees or concurrent-serving capacity claims.

- [Direct semantic review and rejected attempts](../test/evaluation/reports/p1-accepted-agent-review-20261003.json)
- [Daily answers](../test/evaluation/reports/p1-accepted-daily-20261003.json)
- [Broader answers](../test/evaluation/reports/p1-accepted-broader-20261003.json)
- [Serving provenance](../test/evaluation/reports/p1-accepted-provenance-20261003.json)
- [Golden retrieval](../test/evaluation/reports/p1-accepted-retrieval-golden-20261003.json)
- [Broader retrieval](../test/evaluation/reports/p1-accepted-retrieval-broader-20261003.json)
- [Registration](../test/evaluation/reports/p1-plan-20261003.json)

## Implemented correctness boundaries

History persists an explicitly selected cited source section. Follow-up retrieval
carries that subject independently of optional rewriting; generation receives
the original question, a subject label and all admitted sources in retrieval
order. Named topic changes clear the selection. The label is not factual evidence.

A narrowly recognized conditional event appearing only in a competing sibling
section is declined for the selected section. The sample documents Auto crash
loss but no Manual crash-recovery sequence, so the answer states that limit.
Literal IP requests with no valid documented address also decline. Unknown
phrasing and evidence outside these guards still rely on generation.

Conservative citation correction uses complete literal assertions, heading
properties, explicit labels/lists or query-anchored table cells. Valid old
flattened Markdown tables are restored to rows without changing stored source
identity. Exact copied formulas retain a recorded scalar domain restriction.
API and evaluator share narrow complete-excerpt comparison paths; evaluator
records whether the answer came from a literal comparison or the model.
Streaming, replay and persisted answers use the same corrections. Paraphrases,
ambiguous sources and arbitrary entailment are not automatically validated.

Ollama thinking chunks are consumed separately, final-chunk answer text is kept,
and empty completion is an error. No new model, sidecar, serving loop or
self-critic was added. See [research](rag-failure-research.md) and
[chat implementation boundaries](../internal/core/conversation/chat/README.md).

## Reindexing the working source policy safely

Source-SHA deduplication skips unchanged files, so restarting or pressing Import
cannot upgrade their chunks. The existing index was migrated with fresh
collections after acceptance; the original index was retained.

- Copied config: `.local/local-v1/config.yaml`.
- New documents: `documents_chunks_local_v1_20261003` (versioned active alias).
- New cache: `search_cache_local_v1_20261003`.
- Existing history: `chat_history`, retained without resetting it.
- 15 real notes imported: 14 configured samples and the exact recovered
  `business.md` upload. Every original SHA matches the old inventory.
- 32 exact synthetic load-test originals are archived under
  `.local/local-v1/originals/`, outside the new reading corpus.
- Original document generation, active alias, cache and shipped config remain
  available for rollback. Existing history counts match after test-session cleanup.

Launch the migrated corpus from the repository root:

```bash
NADIR_CONFIG="$PWD/.local/local-v1/config.yaml" \
QDRANT_COLLECTION=documents_chunks_local_v1_20261003 ./scripts/local.sh
```

`QDRANT_COLLECTION` overrides YAML, hence its explicit value above. Other role
and history environment overrides still apply; keep them consistent with the
copied configuration. Start the dashboard with its documented dev command.
The copied config keeps upload-only reconciliation: ordinary sample re-import
preserves the recovered upload. A deliberate reset would require re-uploading
`.local/local-v1/originals/business.md`.

For rollback, stop that launcher and run `./scripts/local.sh` with
`QDRANT_COLLECTION=documents_chunks` and the original configuration. Do not
retire old collections or delete the local originals without an owner decision.
For later migrations, repeat the same copied-config/fresh-collection process
and verify all desired originals first.

- [Original recovery and hash checks](../test/evaluation/reports/p1-migration-preflight-20261003.json)
- [Import, inventory, answer review and rollback preservation](../test/evaluation/reports/p1-migration-20261003.json)
- [Persisted Subject and inventory after restart](../test/evaluation/reports/p1-migration-restart-20261003.json)

Raw migration answers may contain owner-note excerpts and remain in ignored
`.local/local-v1/evidence/`; tracked reports contain operational/review metadata.
Temporary validation APIs and the isolated test Qdrant are stopped after checks.
The owner's persistent storage and migrated collections remain. The final
cleanup check found owner Qdrant stopped; the launcher above starts it.
[Cleanup record](../test/evaluation/reports/p1-cleanup-20261003.json) confirms
old and new collection storage exists without restarting that service.

## Historical evidence and rejected experiments

October 1 established mixed upload, duplicate skipping, replacement/cache,
cancellation, saved history/inventory, isolated reset/reindex and browser replay.
The frontend is unchanged by P1. Its [lifecycle](../test/evaluation/reports/daily-use-lifecycle-20261001.json),
[validation](../test/evaluation/reports/daily-use-validation-20261001.json) and
[October 2 browser proof](../test/evaluation/reports/quality-citation-20261002.jpg)
remain dated evidence; P1 did not rerun the browser suite.

October 2's prefix-corrected arm passed retrieval but failed semantic acceptance
at 28/30, 5/5, 2/5 and 49/56, 7/8. Its [review](../test/evaluation/reports/quality-prefix-agent-review-20261002.json)
and [fresh control](../test/evaluation/reports/quality-agent-review-20261002.json)
remain separate historical measurements. All-ancestor headings, scope filtering,
stronger answer plans and model experiments caused regressions and were rejected.
October 3's malformed table-fragment panic was fixed and tested; the rejected
`p1-coverage` report is not accepted evidence. Reports named `p1-final`,
`p1-bounded` and `p1-verified` are also superseded; use `p1-accepted`. Pruned raw scratch arms have a local
backup; retained historical evidence and retention rules are indexed in the
[report catalog](../test/evaluation/reports/README.md).

The installed 8B CPU-embedding experiment solved observed runner eviction, but
its full semantic review still failed. Optional `embedder.num_gpu` remains unset
by default; explicit zero preserves CPU placement. Changing placement needs
measurement, not new model downloads. See its [direct-provider review](../test/evaluation/reports/quality-cpu-direct-smoke-review-20261002.json).
The laptop's 6 GiB GPU and about 15 GiB RAM support the selected single-user
workflow. Earlier eight-stream serving queues and GPU reranker competition do
not define this release.

## Remaining priorities and saved owner questions

Next save ten common questions against actual owner notes before testing and
review useful supported answers versus clear limitations. Finite synthetic
acceptance cannot establish personal usefulness. The recovered upload is now
available, but this is still simulated testing.

Saved source-coverage question: should actual notes include an authoritative
explanation of Manual acknowledgement crash recovery? Until it is indexed, the
app should state the limitation. This does not block the completed P1 fixes.

Independent human review of the exported 39-case judge packet remains required
before calling automated scores calibrated ([issue #13](https://github.com/Chandra179/nadir/issues/13)).
The coordinated frontend upgrade from [PR #15](https://github.com/Chandra179/nadir/pull/15)
is integrated with the brace-expansion fix. October 3–4 local validation on Node
24.19.0 passes typecheck, lint, all 11 unit tests, production build and the mocked
browser workflow; two opt-in live-stack browser tests were skipped. The npm
audit reports zero vulnerabilities. Full scoped Go race tests, vet, API build,
38 Python tests (one optional skip), and the API container build/startup smoke
check pass. These are maintenance checks, not a new live RAG measurement.

Hosted CI still needs account-owner action: [October 3 run 37092111115](https://github.com/Chandra179/nadir/actions/runs/37092111115)
never started its jobs because the account is locked by a billing issue. The
current account balance is unverified. Ten agent-authored questions about the
recovered owner note are saved privately before testing; owner confirmation and
review remain pending. [Owner and calibration review instructions](owner-review.md)
make both human steps explicit.
These do not block local implementation or direct app review. Public deployment,
authentication and distributed coordination remain separate future scope.
