# Retained evaluation evidence

Use this catalog to distinguish accepted measurements from historical controls
and rejected experiments. Retained raw JSON is immutable: keep its original
file name, contents and any referenced fixture/provenance/review. A historical
result does not describe the current implementation merely because its name
contains `final`, `verified` or `shipping`.

## Current finite local acceptance, October 3

The 16 P1 artifacts below record the accepted fixed-question runs, registration,
review, serving provenance, migration and cleanup. Daily: 30/30 supported,
5/5 declines, 5/5 follow-ups. Broader: 56/56 supported, 8/8 declines. The three
fragile repeats contribute 39 supported turns. Reviews are Codex reviews of
synthetic questions, not independent human calibration or owner-use validation.
The control was saved on October 2; it was not freshly recreated October 3.

See [local acceptance](../../../docs/local-v1.md) and
[requirements/evidence](../../../docs/p1-evidence.md) for full limitations.
Source/model/binary hashes describe the measured run, not later maintenance.

- [Registered requirements and comparison bars](p1-plan-20261003.json)
- [Daily API answers and citations](p1-accepted-daily-20261003.json)
- [Broader API answers and citations](p1-accepted-broader-20261003.json)
- [Focused reproduction checks](p1-accepted-focused-20261003.json)
- [Fragile repetition 1](p1-accepted-fragile-1-20261003.json)
- [Fragile repetition 2](p1-accepted-fragile-2-20261003.json)
- [Fragile repetition 3](p1-accepted-fragile-3-20261003.json)
- [Direct review, control comparison and rejected-attempt dispositions](p1-accepted-agent-review-20261003.json)
- [Measured source, configuration, models and serving binary](p1-accepted-provenance-20261003.json)
- [Golden retrieval, three runs](p1-accepted-retrieval-golden-20261003.json)
- [Broader retrieval, three runs](p1-accepted-retrieval-broader-20261003.json)
- [Original-note recovery and hashes](p1-migration-preflight-20261003.json)
- [Fresh collection migration and separate 15-note review](p1-migration-20261003.json)
- [Persisted Subject and inventory after restart](p1-migration-restart-20261003.json)
- [Dated implementation checks](p1-validation-20261003.json)
- [Temporary-service cleanup and preserved owner storage](p1-cleanup-20261003.json)

## Historical evidence

These reports remain because a retained document, architecture decision,
review, calibration packet or comparison depends on them. Read their dates,
corpus/configuration and limitations before comparing scores. In particular,
October 2's rejected prefix/8B candidates and September's uncalibrated judge
scores are not current acceptance. Some retained diagnostic reviews reference
several earlier arms; those dependencies are kept together.

### Historical architecture-decision and judge evidence

- [chunk-2048-fullcorpus-20260929-run1.json](chunk-2048-fullcorpus-20260929-run1.json)
- [chunk-2048-fullcorpus-20260929-run2.json](chunk-2048-fullcorpus-20260929-run2.json)
- [chunk-recursive-goldencorpus-20260927.json](chunk-recursive-goldencorpus-20260927.json)
- [chunk-sentencewindow-goldencorpus-20260927.json](chunk-sentencewindow-goldencorpus-20260927.json)
- [chunker-fix-512-fullcorpus-20260929-run1.json](chunker-fix-512-fullcorpus-20260929-run1.json)
- [chunker-fix-512-fullcorpus-20260929-run2.json](chunker-fix-512-fullcorpus-20260929-run2.json)
- [generation-gemma1b-fixes-fullcorpus-20260929.json](generation-gemma1b-fixes-fullcorpus-20260929.json)
- [generation-gemma4b-fullcorpus-20260929.json](generation-gemma4b-fullcorpus-20260929.json)
- [generation-representative-ctx1400-20260930.json](generation-representative-ctx1400-20260930.json)
- [generation-representative-defaults-20260930.json](generation-representative-defaults-20260930.json)
- [pre-fix-baseline-512-fullcorpus-20260929.json](pre-fix-baseline-512-fullcorpus-20260929.json)
- [pre-fix-rerank-gpu-fullcorpus-20260929.json](pre-fix-rerank-gpu-fullcorpus-20260929.json)

### Lifecycle, browser and dependency follow-up

- [daily-use-lifecycle-20261001.json](daily-use-lifecycle-20261001.json)
- [daily-use-local-v1-20261001.json](daily-use-local-v1-20261001.json)
- [daily-use-validation-20261001.json](daily-use-validation-20261001.json)
- [frontend-pr15-audit-fix.patch](frontend-pr15-audit-fix.patch)
- [frontend-pr15-review-20261001.json](frontend-pr15-review-20261001.json)

### Selected September 30 benchmark baselines

- [load-defaults-20260930.json](load-defaults-20260930.json)
- [retrieval-golden-defaults-20260930.json](retrieval-golden-defaults-20260930.json)
- [user-paths-defaults-20260930.json](user-paths-defaults-20260930.json)

### Pre-fix diagnosis and fixed fixture dependencies

- [p1-default-focused-20261003.json](p1-default-focused-20261003.json)
- [p1-default-focused-fixture-20261003.json](p1-default-focused-fixture-20261003.json)
- [p1-default-focused-review-20261003.json](p1-default-focused-review-20261003.json)
- [p1-verified-focused-fixture-20261003.json](p1-verified-focused-fixture-20261003.json)

### October 2 controls, reviews and their dependencies

- [quality-agent-review-20261002.json](quality-agent-review-20261002.json)
- [quality-citation-20261002.jpg](quality-citation-20261002.jpg)
- [quality-control-daily-20261002.json](quality-control-daily-20261002.json)
- [quality-control-representative-answers-20261002.json](quality-control-representative-answers-20261002.json)
- [quality-control-representative-retrieval-20261002.json](quality-control-representative-retrieval-20261002.json)
- [quality-control-retrieval-20261002.json](quality-control-retrieval-20261002.json)
- [quality-cpu-direct-provenance-20261002.json](quality-cpu-direct-provenance-20261002.json)
- [quality-cpu-direct-residence-20261002.json](quality-cpu-direct-residence-20261002.json)
- [quality-cpu-direct-smoke-20261002.json](quality-cpu-direct-smoke-20261002.json)
- [quality-cpu-direct-smoke-review-20261002.json](quality-cpu-direct-smoke-review-20261002.json)
- [quality-cpue-model-agent-review-20261002.json](quality-cpue-model-agent-review-20261002.json)
- [quality-cpue-model-daily-20261002.json](quality-cpue-model-daily-20261002.json)
- [quality-cpue-model-representative-answers-20261002.json](quality-cpue-model-representative-answers-20261002.json)
- [quality-focused-representative-retrieval-20261002.json](quality-focused-representative-retrieval-20261002.json)
- [quality-focused-retrieval-20261002.json](quality-focused-retrieval-20261002.json)
- [quality-github-status-20261002.json](quality-github-status-20261002.json)
- [quality-plan-20261002.json](quality-plan-20261002.json)
- [quality-prefix-agent-review-20261002.json](quality-prefix-agent-review-20261002.json)
- [quality-prefix-central-evidence-audit-20261002.json](quality-prefix-central-evidence-audit-20261002.json)
- [quality-prefix-daily-20261002.json](quality-prefix-daily-20261002.json)
- [quality-prefix-representative-answers-20261002.json](quality-prefix-representative-answers-20261002.json)
- [quality-prefix-representative-retrieval-20261002.json](quality-prefix-representative-retrieval-20261002.json)
- [quality-prefix-retrieval-20261002.json](quality-prefix-retrieval-20261002.json)
- [quality-repeat-questions-20261002.json](quality-repeat-questions-20261002.json)
- [quality-retained-daily-20261002.json](quality-retained-daily-20261002.json)
- [quality-retained-repeat-1-20261002.json](quality-retained-repeat-1-20261002.json)
- [quality-retained-repeat-2-20261002.json](quality-retained-repeat-2-20261002.json)
- [quality-retained-repeat-3-20261002.json](quality-retained-repeat-3-20261002.json)
- [quality-retained-representative-answers-20261002.json](quality-retained-representative-answers-20261002.json)
- [quality-scoped-representative-retrieval-20261002.json](quality-scoped-representative-retrieval-20261002.json)
- [quality-scoped-retrieval-20261002.json](quality-scoped-retrieval-20261002.json)
- [quality-scopedlabel-representative-retrieval-20261002.json](quality-scopedlabel-representative-retrieval-20261002.json)
- [quality-scopedlabel-retrieval-20261002.json](quality-scopedlabel-retrieval-20261002.json)

## Retention policy

1. Write exploratory runs to ignored `.local/evaluation/`. The evaluator uses
   that directory by default; the API runner accepts it through `--report`.
2. Promote an accepted run or a decision-changing rejected comparison with
   fixed fixture/corpus hashes, role/config/model identity, registration,
   raw outputs, review and limitations. Preserve the control and all evidence
   dependencies. Owner-note excerpts stay local unless publication is approved.
3. Keep evidence used by current acceptance, accepted ADRs, regression diagnosis
   or outstanding work. Keep the exact calibration source report and fixture
   while the blind review packet is pending.
4. Prune superseded scratch outputs after recording the disposition and checking
   inbound references and hashes. Never treat a structural citation check as a
   semantic review, or an old source/binary hash as proof of a newer build.

## October 3 maintenance cleanup

The folder held 229 artifacts (225 JSON, three screenshots and one patch),
about 72.3 MiB. This cleanup retained 73 artifacts, about 28.3 MiB, and removed
156 superseded/unreferenced scratch artifacts, about 44.0 MiB. All retained
raw artifact bytes and fixture hashes are unchanged. The README is additional
navigation, not a new evaluation result. All 35 ADRs are retained.

Before removal, every artifact was saved and hash-verified in ignored
`.local/report-archive/20261003-cleanup/before-cleanup.tar.gz`. The neighboring
`inventory.json` and `selection.json` record original hashes and the exact
retention/removal selection. These local backups are not distributed with Git;
previously committed artifacts also remain in Git history. The archive includes
the earlier TODO and evaluation docs for historical context.

To restore one pruned file locally from the repository root (after checking it
will not overwrite a newer file):

```bash
tar -xzf .local/report-archive/20261003-cleanup/before-cleanup.tar.gz \
  test/evaluation/reports/REPORT_NAME.json
```

Maintenance verification: the scoped Go unit suite (`-short -count=1`) and vet
pass; evaluator help and both fixture schemas were checked; retained artifact
and calibration-source hashes match; Markdown file links and references to
removed reports have no missing targets; `git diff --check` passes. No live
answer-quality or latency evaluation was rerun for this maintenance change.
