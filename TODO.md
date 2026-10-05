# TODO

Finish line: a local Markdown document-reading app on the current
laptop, operated by one person. Keep the installed Gemma 3 4B generator,
Gemma 3 1B rewriter and EmbeddingGemma embedder. No further model downloads.

## Next: validate usefulness with chosen documents

- [ ] Choose documents the app should answer from, upload or configure them,
  and save realistic general questions with enough detail to guide the answer.
  The documents need not be the user's own notes. The ten saved business-note
  questions are unconfirmed agent proposals, not the required review corpus.
  Record source hashes and questions, then review answers in the dashboard.
- [ ] Review each answer and citation for usefulness, supported facts and clear
  limitations. Fix reproduced failures in the responsible layer and add focused
  regressions. Synthetic sample acceptance does not establish usefulness for
  the chosen documents and questions.

## Completed local engineering acceptance

Sample auto-import is disabled in local and Compose startup. The current
index/cache were cleared; normal startup selects an empty reading index.
History and the older rollback index remain. These are October 5 maintenance
changes; they are not a new answer-quality measurement.

P1 passed its finite checklist on October 3: daily 30/30 supported, 5/5 declines,
5/5 follow-ups; broader 56/56 supported and 8/8 declines. These are direct Codex
reviews of saved synthetic questions, not independent human judgments.

- [Dated acceptance evidence, supporting reports and archive](test/evaluation/reports/README.md)
