# TODO

Finish line: a personal/local Markdown technical-notes app on the current
laptop, operated by one person. Keep the installed Gemma 3 4B generator,
Gemma 3 1B rewriter and EmbeddingGemma embedder. No further model downloads.

## Next: validate personal usefulness

- [ ] Save ten common questions against actual owner notes before running them.
- [ ] Review each answer and citation for usefulness, supported facts and clear
  limitations. Fix reproduced failures in the responsible layer and add focused
  regressions. Synthetic sample acceptance does not establish owner usefulness.

Saved coverage question: should the notes include an authoritative explanation
of Manual acknowledgement crash recovery? Until that source is indexed, retain
Manual selection and decline its undocumented outcome.

## Administrative follow-up

- [ ] Restore hosted Actions access and verify current status. The last saved
  observation is October 2: the newest run was September 30 and blocked by
  billing; current account balance is unverified. See the
  [dated status](test/evaluation/reports/quality-github-status-20261002.json).
- [ ] Refresh and merge [PR #15](https://github.com/Chandra179/nadir/pull/15)
  after hosted checks are available. Rebase over the brace-expansion lockfile
  fix; retain the [prepared patch](test/evaluation/reports/frontend-pr15-audit-fix.patch).
  The dependency batch also addresses the remaining Vitest 3 mocker advisory.
- [ ] Complete independent human review of the
  [39-case judge packet](test/evaluation/judge-calibration/20260930-phi4-mini/manifest.json)
  before claiming calibrated judge scores
  ([issue #13](https://github.com/Chandra179/nadir/issues/13)). This does not
  block the local workflow or direct app review.

## Completed local engineering acceptance

P1 passed its finite checklist on October 3: daily 30/30 supported, 5/5 declines,
5/5 follow-ups; broader 56/56 supported and 8/8 declines. These are direct Codex
reviews of saved synthetic questions, not independent human judgments.

- [Acceptance, limits and copied-config launch/rollback](docs/LOCAL_V1.md)
- [Requirements and dated verification evidence](docs/P1_EVIDENCE.md)
- [Retained current and historical reports](test/evaluation/reports/README.md)
- [Research behind the fixes](docs/RAG_FAILURE_RESEARCH.md)

## Optional work, only with a concrete need

PDF intake benchmarking, profiling, public-math research and an OpenAPI source
remain optional. Retrieval experiments require a reproduced shortcoming and
measured gains. Keep reranking, custom fusion and contextual enrichment off by
default.

Public/multiuser deployment would require authentication, tenant isolation,
shared event logs, distributed mutation fencing/indexing leases, central
telemetry and tested backup/restore. It is outside the current finish line;
current coordination applies to one API process.
