# Retained evaluation evidence

This catalog contains **31 reports**, about **10.1 MiB** including their three
supporting question fixtures. These are dated measurements, not proof that the
current configuration or a newly uploaded corpus has been evaluated. The latest
recorded local engineering acceptance is **October 3, 2026**.

## Structure and original identities

Reports use `YYYY-MM-DD/run-name/artifact.ext`, with lowercase names separated
by hyphens. Repeated runs use two digits, such as `answers-fragile-01.json`.
Comparison arms live under `control/` and `candidate/` within their run group.
Question inputs live in the corresponding dated groups under `../fixtures/`.
The stable reusable fixtures, such as `../golden.json`, keep their existing paths.

All retained raw report and question-fixture bytes are unchanged. The
[manifest](manifest.json) maps each original repository-relative path to its
new `path`, exact `sha256`, size and retention reason. Paths recorded inside
frozen reports retain their original historical identity; look up those paths
in `artifacts[].original_path` to find the current file. An archived artifact
has `path: null` and is recoverable using the instructions below.

## Local acceptance measured October 3

The 16 artifacts below record registration, accepted fixed-question runs,
review, serving provenance, migration and cleanup. Daily: 30/30 supported,
5/5 declines, 5/5 follow-ups. Broader: 56/56 supported, 8/8 declines. Three
fragile repeats contribute 39 supported turns. Reviews are Codex reviews of
synthetic questions, not independent human calibration or usefulness validation.
The control was saved October 2 and was not rerun October 3. Source/model/binary
hashes describe that measurement, not later maintenance.

The direct review below records the observed failures, comparison limits and
remaining validation work.

- [Registered requirements and comparison bars](2026-10-03/local-acceptance/plan.json)
- [Daily answers and citations](2026-10-03/local-acceptance/answers-daily.json)
- [Broader answers and citations](2026-10-03/local-acceptance/answers-broader.json)
- [Focused reproduction checks](2026-10-03/local-acceptance/answers-focused.json)
- [Fragile repetition 01](2026-10-03/local-acceptance/answers-fragile-01.json)
- [Fragile repetition 02](2026-10-03/local-acceptance/answers-fragile-02.json)
- [Fragile repetition 03](2026-10-03/local-acceptance/answers-fragile-03.json)
- [Direct review, control comparison and rejected-attempt dispositions](2026-10-03/local-acceptance/review.json)
- [Measured source, configuration, models and serving binary](2026-10-03/local-acceptance/provenance.json)
- [Golden retrieval, three runs](2026-10-03/local-acceptance/retrieval-golden.json)
- [Broader retrieval, three runs](2026-10-03/local-acceptance/retrieval-broader.json)
- [Original-note recovery and hashes](2026-10-03/local-acceptance/migration-preflight.json)
- [Fresh collection migration and separate 15-note review](2026-10-03/local-acceptance/migration.json)
- [Persisted Subject and inventory after restart](2026-10-03/local-acceptance/migration-restart.json)
- [Dated implementation checks](2026-10-03/local-acceptance/validation.json)
- [Temporary-service cleanup and preserved storage](2026-10-03/local-acceptance/cleanup.json)

## Required historical support

October 2's control review contains both its control and an earlier candidate.
Its five candidate outputs remain so the unchanged review stays traceable;
those candidate results are not accepted evidence.

| Group | Retained files |
|---|---|
| October 2 comparison | [Review](2026-10-02/quality-comparison/review.json); control [daily answers](2026-10-02/quality-comparison/control/answers-daily.json), [broader answers](2026-10-02/quality-comparison/control/answers-broader.json), [golden retrieval](2026-10-02/quality-comparison/control/retrieval-golden.json), [broader retrieval](2026-10-02/quality-comparison/control/retrieval-broader.json) |
| Dependencies of that review | Candidate [daily answers](2026-10-02/quality-comparison/candidate/answers-daily.json), [broader answers](2026-10-02/quality-comparison/candidate/answers-broader.json), repetitions [01](2026-10-02/quality-comparison/candidate/answers-repeat-01.json), [02](2026-10-02/quality-comparison/candidate/answers-repeat-02.json), [03](2026-10-02/quality-comparison/candidate/answers-repeat-03.json) |
| October 3 focused baseline | [Answers](2026-10-03/focused-baseline/answers-focused.json), [review](2026-10-03/focused-baseline/review.json) |
| October 1 local workflow | [Daily answers](2026-10-01/local-workflow/answers-daily.json), [lifecycle](2026-10-01/local-workflow/lifecycle.json), [validation](2026-10-01/local-workflow/validation.json) |

The three relocated inputs also retain their exact bytes and hashes:

- [Fragile repetition questions](../fixtures/2026-10-02/fragile-repeats/questions.json)
- [Focused baseline questions](../fixtures/2026-10-03/focused-baseline/questions.json)
- [Accepted focused questions](../fixtures/2026-10-03/local-acceptance/questions-focused.json)

## Archived experiments and restoration

The October 5 cleanup archives **39 artifacts**, about **18.2 MiB**:
September chunking/model experiments, older standalone benchmarks, frontend
dependency review, superseded October 2 prefix/model/diagnostic arms, and the
missing browser screenshot and September 30 generation source report.
Their outcomes remain summarized in ADRs
[0033](../../../docs/adr/0033-default-chunker-recursive.md),
[0034](../../../docs/adr/0034-chunker-fixes-and-size.md), and
[0035](../../../docs/adr/0035-source-scopes-and-local-quality-candidate.md).
The manifest records each removed artifact's original path and hash.

The missing screenshot and source report were retired at the user's request.
The dependent calibration manifest and two blank reviewer packets were removed
from the active tree, together with two generators for the removed sample
corpus. Their verified backups are under ignored
`.local/report-archive/2026-10-05-retired-missing-evidence/`:
`before-retirement.tar.gz` preserves the packet and preceding documentation;
`before-dependency-cleanup.tar.gz` preserves the generators and preceding code.
The manifest's `retirement` section records their paths and hashes.

Before any move or removal, all 73 artifacts, evaluation inputs, calibration
packet and documentation were backed up and verified against their SHA-256
hashes in ignored
`.local/report-archive/2026-10-05-key-evidence/before-cleanup.tar.gz`.
Its `inventory.json` and `selection.json` record the snapshot and selection.
The manifest records the archive hash and pre-cleanup Git revision. Local
backups are not distributed with Git; committed originals also remain at that
revision in Git history.

To recover a file without overwriting the organized tree, find its
`original_path` in the manifest and extract it into a temporary directory:

```bash
mkdir -p /tmp/nadir-report-restore
tar -xzf .local/report-archive/2026-10-05-key-evidence/before-cleanup.tar.gz \
  -C /tmp/nadir-report-restore test/evaluation/reports/REPORT_NAME.json
```

The earlier October 3 cleanup reduced 229 artifacts (about 72.3 MiB) to 73
(about 28.3 MiB). Its 156 pruned files remain in the separate ignored
`.local/report-archive/20261003-cleanup/before-cleanup.tar.gz` backup. The
October 5 archive contains the 73-artifact tree, not those earlier pruned files.

The source corpus has also been removed. Historical source paths in unchanged
reports and question fixtures describe that dated corpus, not current files.
Original source files remain in Git history at `source_git_revision`; live
evaluations require a question set and an available matching corpus.

## Retention policy

1. Write exploratory runs to ignored `.local/evaluation/`, the evaluator's
   default output directory. API performance reports belong in ignored
   `.local/benchmark/`; see the [Locust guide](../../../benchmark/README.md).
2. Promote key accepted evidence and its required controls, reviews, provenance
   and fixed inputs using the dated structure above. Record identities and
   retention reasons in the manifest. Private document excerpts stay local.
3. Preserve raw bytes when renaming evidence. Update navigation and executable
   metadata paths without changing measured
   results, source hashes or frozen reviewer content.
4. Archive superseded experiments after recording their disposition and
   checking dependencies. A calibration packet needs its exact source and
   fixture until completed or explicitly retired. Never present old results
   as a new measurement.

## Maintenance verification, October 5

The original backup's 137 members and all 34 active retained artifacts match
their hashes. The six reusable fixtures are unchanged. Retired support files
were backed up and verified before removal. Documentation links, active input
references, manifest counts and formatting were checked after retirement.

The initial cleanup ran the focused Go evaluator suites and both Python
question-runner tests. Tests and live answer-quality or latency evaluations
were not rerun for retirement. Current retirement checks are saved in ignored
`.local/report-archive/2026-10-05-retired-missing-evidence/verification.json`.

The October 5 Locust migration also retired the legacy benchmark tools and
saved-answer runner after a separate verified backup. Their historical paths
inside raw evidence describe the original measurement. Tool restoration and
current benchmark commands are in the [Locust guide](../../../benchmark/README.md).
