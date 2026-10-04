# TODO

Finish line: a personal/local Markdown technical-notes app on the current
laptop, operated by one person. Keep the installed Gemma 3 4B generator,
Gemma 3 1B rewriter and EmbeddingGemma embedder. No further model downloads.

## Next: validate personal usefulness

- [ ] Confirm or replace the ten saved agent drafts with common owner questions
  before running them. The private packet is
  `.local/local-v1/owner-review/questions.json`; [review instructions](docs/owner-review.md)
  cover source hashes, execution and review. No owner confirmation is recorded.
- [ ] Review each answer and citation for usefulness, supported facts and clear
  limitations. Fix reproduced failures in the responsible layer and add focused
  regressions. Synthetic sample acceptance does not establish owner usefulness.

Saved coverage question: should the notes include an authoritative explanation
of Manual acknowledgement crash recovery? Until that source is indexed, retain
Manual selection and decline its undocumented outcome.

## Administrative follow-up

- [ ] Complete independent human review of the
  [39-case judge packet](test/evaluation/judge-calibration/20260930-phi4-mini/manifest.json)
  before claiming calibrated judge scores
  ([issue #13](https://github.com/Chandra179/nadir/issues/13)). This does not
  block the local workflow or direct app review. See the
  [two-reviewer instructions](docs/owner-review.md).

## Completed local engineering acceptance

P1 passed its finite checklist on October 3: daily 30/30 supported, 5/5 declines,
5/5 follow-ups; broader 56/56 supported and 8/8 declines. These are direct Codex
reviews of saved synthetic questions, not independent human judgments.

- [Acceptance, limits and copied-config launch/rollback](docs/local-v1.md)
- [Requirements and dated verification evidence](docs/p1-evidence.md)
- [Retained current and historical reports](test/evaluation/reports/README.md)
- [Research behind the fixes](docs/rag-failure-research.md)
