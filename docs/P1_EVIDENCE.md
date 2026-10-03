# Fitness verification: P1 local correctness, 2026-10-03

Specification: [personal/local acceptance criteria](LOCAL_V1.md), the P1 requirements
enumerated below, and the fixed bars/invariants in the
[registered plan](../test/evaluation/reports/p1-plan-20261003.json).
The measured implementation was the working tree over `f9f3a8b` at the accepted
run. Source and serving-binary hashes are recorded in its dated provenance;
later maintenance changes do not constitute a new live measurement. No new model
was downloaded for these fixes; the earlier authorized Qwen comparison was not
adopted.

| ID | Type | Requirement | Status | Evidence |
|---|---|---|---|---|
| local-setup | outcome | Configured enabled models are checked at their own endpoints; unavailable models prevent readiness | VERIFIED | Current full configuration/readiness/provider tests; current live readiness in serving provenance. Setup/browser launch evidence remains dated October 1. |
| local-import | outcome | Visible successful sources, duplicate skipping, per-file failures, disabled PDF safety | VERIFIED | Current runtime/indexing/HTTP tests; October 1 mixed-import report; current migration imports 14 configured files and one recovered upload, zero failures, then skips all 14 configured unchanged files. |
| local-update | invariant | Replacement activates new source evidence and invalidates cache | VERIFIED | Current full indexing/cache/store tests; October 1 live replacement and repeated-cache canary. No new live replacement was claimed for October 3. |
| local-lifecycle | outcome | Cancellation persists partial answers; restart retains history/inventory; browser replay/citations work | VERIFIED | Current full chat/HTTP tests; current live Subject/inventory restart; October 1 cancellation/history/browser lifecycle and October 2 browser proof. P1 has no frontend changes and did not rerun the browser suite. |
| local-supported | outcome | At least 27/30 supported useful answers | VERIFIED | Direct review: 30/30 daily. Actual question and cited evidence inspected, not just a fixture term or mapped ID. |
| local-declines | outcome | 5/5 absent private/live values declined without sample substitution | VERIFIED | Daily 5/5; broader 8/8; capability tests and live narrow literal-IP decline. |
| local-followups | outcome | 5/5 intended subjects preserved with supported answers or limitations | VERIFIED | Daily 5/5; Manual remains selected and declines its undocumented conditional outcome. |
| p1-subject | outcome | Selected source section survives rewrite settings, follow-ups, decline and restart; topic changes clear it | VERIFIED | Reference/service regressions cover optional rewriting off, rejected-option explanations, ambiguous ordered comparisons and topic switches. Current Qdrant live restart reads persisted Manual Subject and gives the scoped decline afterward. Legacy missing Subject roundtrip remains supported. |
| p1-attribution | outcome | Observed material citation errors use their actual supporting sections consistently across stream/history/evaluator | VERIFIED | Complete literal assertion/property/list/table regressions and full app review fix Secant, expired locks, backpressure, sine/radian cells and metadata. Evaluator shares correction; unknown and paraphrased claims are not automatically certified. |
| p1-completeness | outcome | Expressly requested alternatives and recorded restrictions survive | VERIFIED | Full manifest, graph taxonomy, AP/CP and fintech answers inspected; exact integral formula retains n ≠ -1. Generic comparison/table/formula tests cover ambiguity, altered claims, units, conditions and missing evidence. |
| p1-absent-conditional | outcome | Do not infer missing Manual crash recovery or substitute Auto mode | VERIFIED | Current daily follow-up, all three fragile repetitions and migrated restart decline for Manual. Other forms and general answerability are outside the guard's proven boundary. |
| p1-full-packs | invariant | Both unchanged full app packs and three fragile repetitions complete | VERIFIED | 40 + 64 + 13×3 = 143 turns; zero operational failures and unmapped IDs; direct review of all outputs. |
| p1-no-regression | constraint | No new material failures against saved fresh control | VERIFIED | Per-case comparison with the saved fresh October 2 control (23/30, 5/5, 4/5; 47/56, 5/8). No new material failure found. This is a historical control comparison, not a newly run control or independent review. |
| p1-retrieval | constraint | Golden/broader Hit/MRR/nDCG drop no more than 0.01; three runs per pack | VERIFIED | 133×3 + 64×3 = 591 requests. Golden Hit/MRR/nDCG 1.0000/0.8504/0.8640; broader 0.9464/0.8229/0.8544. Registered bars pass. Golden Recall is 0.9825; broader distractor exposure remains 0.2344 versus old control 0.2188. |
| p1-fixed-inputs | invariant | Preserve saved questions, corpus hashes and original generation question | VERIFIED | Runner rejects changed sample hashes; accepted fixture hashes match registration/control. Current service/prompt tests preserve original questions and isolate reference context. |
| p1-source-identity | invariant | Ranked sources, versioned identity, anchors and bounded budgets remain intact | VERIFIED | Current prompt/source tests preserve retrieval order and citation identity; valid table presentation changes do not mutate stored chunks. All unchanged context/output/queue settings recorded in provenance. No source filtering was introduced. |
| p1-local-resources | constraint | Use existing hardware/default models; no added serving service or model download | VERIFIED | Installed role model digests/config recorded. Single sequential app replay on current laptop; reranker/fusion settings unchanged. No claim of eight-stream capacity or universal cold-load latency. |
| p1-protocol | invariant | Thinking is separate; final answer chunks kept; empty completions are errors | VERIFIED | Current generator protocol/error regressions, full Go tests and changed-package race tests. |
| p1-migration | outcome | After acceptance, copied config/fresh docs and cache; all desired originals available; preserve old data/history | VERIFIED | All 47 original hashes match the old inventory. 15 real notes imported, 32 exact load-test originals archived. Old generation/alias/cache/config retained, old collection/history counts unchanged after owned-session cleanup. Fresh inventory has exact expected hashes. |
| p1-migrated-workflow | outcome | Recheck the migrated corpus, duplicate import and restart | VERIFIED | Separate 15-note run: daily 30/30, 5/5, 5/5; broader 56/56, 8/8, no operational errors. Re-import skips 14, recovered upload remains, inventory 15 after restart; selected Manual persists. Private raw answers stay ignored locally. |
| p1-checks | constraint | Required Go checks and formatting pass | VERIFIED | Full and short scoped Go suites, race tests for chat/eval/generator/history, vet, gofmt and git diff --check pass October 3. Tests use dedicated history integration collection, not existing owner sessions. |
| owner-usefulness | outcome | Validate usefulness on ten saved actual owner questions | UNVERIFIED | No owner-authored question packet or owner review exists. Simulated tests, including on the recovered upload, do not establish personal usefulness. This is the next product validation step, separate from P1's finite engineering gates. |
| calibrated-judge | constraint | Independent human calibration before calling judge scores calibrated | NOT APPLICABLE | No calibrated judge/release claim is made. Issue #13's 39-case human review remains outstanding. |
| hosted-public | constraint | Hosted CI, dependency PR, authentication/distributed deployment | NOT APPLICABLE | Personal/local release scope. October 3 hosted run 37092111115 did not start because of an account billing lock; the dependency upgrade passes local maintenance checks. Hosted success remains unverified. |

## Evidence

- [Direct review, criteria, per-case reasons, warnings and rejected attempts](../test/evaluation/reports/p1-accepted-agent-review-20261003.json)
- [Accepted source/config/model/binary provenance](../test/evaluation/reports/p1-accepted-provenance-20261003.json)
- [Golden retrieval](../test/evaluation/reports/p1-accepted-retrieval-golden-20261003.json), [broader retrieval](../test/evaluation/reports/p1-accepted-retrieval-broader-20261003.json)
- [Migration and separate corpus review](../test/evaluation/reports/p1-migration-20261003.json), [persisted Subject restart](../test/evaluation/reports/p1-migration-restart-20261003.json)
- [Current check record](../test/evaluation/reports/p1-validation-20261003.json), [cleanup and preserved storage](../test/evaluation/reports/p1-cleanup-20261003.json)
- [Dated lifecycle evidence](../test/evaluation/reports/daily-use-lifecycle-20261001.json), [dated browser evidence](../test/evaluation/reports/quality-citation-20261002.jpg)

## Conflicts and limits

No required outcome was weakened to pass. Manual crash redelivery is not in the
notes; declining the absent detail satisfies the source-support requirement.
Questions and corpus were not rewritten. Direct review accepts expressly asked
short answers, rather than requiring unasked fixture extras. Some redundant
citation markers remain; warnings are saved. Literal correction does not prove
arbitrary paraphrase support, and the narrow English rules do not certify
unknown question forms.

Earlier model/prompt arms, malformed-table panic and incomplete candidates are
rejected diagnostics. Superseded raw attempts are backed up locally and pruned
according to the [report catalog](../test/evaluation/reports/README.md); the
accepted review retains their dispositions. Only `p1-accepted` supplies current
controlled acceptance.
All reviews are Codex reviews (`human:false`, `independent_human_review:false`,
`release_gate:false`); finite local acceptance is recorded separately.

## Recommendation

Use the copied-config candidate for the measured personal/local workflow. P1's
engineering gates pass. Actual personal usefulness remains unverified and needs
owner testing; no independent calibration or public release is claimed.

Final cleanup confirms both temporary API ports and the disposable container are
absent. The owner Qdrant is currently stopped; its mounted storage contains the
old and migrated document/cache collections and history. Live inventory checks
remain the earlier dated migration result. The copied-config launcher starts
owner Qdrant when the user returns to the app.
