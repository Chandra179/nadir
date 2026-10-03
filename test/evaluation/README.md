# Evaluation fixtures

These JSON files are maintained test inputs. Reviewed outputs live in
[reports/](reports/README.md); new scratch outputs belong in ignored
`.local/evaluation/`.

| Fixture | Purpose |
|---|---|
| [golden.json](golden.json) | 133 synthetic retrieval queries over four manifest-backed sample documents; used by evaluator tests and CLI defaults |
| [representative.json](representative.json) and [representative-corpus.json](representative-corpus.json) | 64 broader retrieval/generation queries over all 14 samples and their source hashes |
| [daily-use-questions.json](daily-use-questions.json) | 30 supported questions, 5 unsupported/private/live requests and 5 follow-ups through the actual chat API |
| [representative-app-questions.json](representative-app-questions.json) | The broader 64 queries expressed as API workflow cases |
| [user-paths.json](user-paths.json) | Semantic-cache and query-rewriting benchmark cases |
| [Judge packet](judge-calibration/20260930-phi4-mini/manifest.json) | 39-case blind human-review packet tied to a specific historical report and fixture by hash |

The questions and relevance labels are synthetic. Separate recorded synthetic
judgment passes are not independent human judgments; `release_gate` remains
false. Reviewer A and B files are intentionally separate blank forms, not
redundant completed reviews. Keep both and their exact source report/fixture
until calibration is completed or explicitly retired.

## Current acceptance, October 3

Direct Codex review of fixed app questions: **30/30 supported, 5/5 declines,
5/5 follow-ups** daily; **56/56 supported, 8/8 declines** broader. Three repeats
of 13 fragile cases also pass. These finite results are not owner-use validation
or calibrated judge scores. See [local acceptance](../../docs/LOCAL_V1.md),
[fitness evidence](../../docs/P1_EVIDENCE.md) and the
[accepted report catalog](reports/README.md).

October 2's corrected-prefix and model experiments are historical rejected
candidates. Their results do not describe the accepted implementation. The
report catalog retains baselines and decisions; superseded scratch arms are
removed from the active tree after a hash-verified local backup.

## Run retrieval checks

From the repository root with Qdrant and Ollama running:

```bash
go run ./cmd/evaluator --no-rerank --runs 3
go run ./cmd/evaluator --golden test/evaluation/representative.json \
  --no-rerank --runs 3 --report .local/evaluation/representative.json
```

Default evaluator output is `.local/evaluation/<unix_ts>.json`. Use
`--ensure-ingest` only when an import of the configured sources is intended;
unchanged-file skipping does not upgrade old chunks after an indexing-policy
change. Use [fresh-collection migration](../../docs/LOCAL_V1.md#reindexing-the-working-source-policy-safely)
for that change.

## Run saved app questions

```bash
python3 scripts/run_daily_use.py --base-url http://127.0.0.1:8100 \
  --report .local/evaluation/daily.json
python3 scripts/run_daily_use.py --base-url http://127.0.0.1:8100 \
  --fixture test/evaluation/representative-app-questions.json \
  --report .local/evaluation/broader.json
```

The runner checks source hashes, refuses to overwrite a report and deletes only
its own sessions unless `--keep-sessions` is used. It captures full answers and
citations. Mapped citation IDs are structural diagnostics; inspect whether the
actual cited passage supports each claim. Owner-note excerpts must stay in
ignored local reports unless separately approved for publication.

## Fixture maintenance and optional research

`scripts/generate_evaluation_candidate.py` and
`scripts/generate_representative_evaluation.py` regenerate synthetic inputs.
Do not change a fixture to make a failing implementation pass. Record changed
source/fixture hashes and evaluate a new baseline when inputs intentionally
change.

See the [evaluator guide](../../internal/eval/README.md) for generation-judge
configuration and release-gate validation. Independent human calibration is
pending. The optional ARQMath importer can build public-math inputs, but that
pack and licensed corpus are not checked in and are outside the local release.
