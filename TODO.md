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

## Recorded maintenance and measurements

Sample auto-import is disabled in local and Compose startup. The current
index/cache were cleared; normal startup selects an empty reading index.
History and the older rollback index remain. These are October 5 maintenance
changes; they are not a new answer-quality measurement.

Historical sample inputs and results were retired on October 6. New quality
checks use questions for chosen documents and the
[Ragas evaluator](eval/README.md); performance runs use
[Locust](benchmark/README.md). Reports are generated under ignored `.local/`.
Usefulness for chosen documents still needs the review described above.
