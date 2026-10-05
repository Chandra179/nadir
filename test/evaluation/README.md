# Evaluation inputs and historical fixtures

These JSON files preserve the inputs behind dated reports and fixture-schema
checks. Their sample source corpus has been removed, so they cannot be rerun
against the current reading index. Live runs require new question sets for an
available matching corpus. Reviewed outputs live in
[reports/](reports/README.md); new scratch outputs belong in ignored
`.local/evaluation/`.

| Fixture | Purpose |
|---|---|
| [golden.json](golden.json) | 133 historical synthetic retrieval queries over four manifest-backed source documents; used by fixture-schema tests |
| [representative.json](representative.json) and [representative-corpus.json](representative-corpus.json) | 64 broader retrieval/generation queries over all 14 samples and their source hashes |
| [daily-use-questions.json](daily-use-questions.json) | 30 supported questions, 5 unsupported/private/live requests and 5 follow-ups through the actual chat API |
| [representative-app-questions.json](representative-app-questions.json) | The broader 64 queries expressed as API workflow cases |
| [user-paths.json](user-paths.json) | Semantic-cache and query-rewriting benchmark cases |
| [Fragile questions](fixtures/2026-10-02/fragile-repeats/questions.json) | Fixed repetition inputs shared by retained October 2 and October 3 reports |
| [Focused baseline](fixtures/2026-10-03/focused-baseline/questions.json) and [accepted focused questions](fixtures/2026-10-03/local-acceptance/questions-focused.json) | Frozen inputs for the focused diagnosis and accepted reproduction checks |

The questions and relevance labels are synthetic. Separate recorded synthetic
judgment passes are not independent human judgments; `release_gate` remains
false. The September 30 calibration packet and both blank reviewer forms were
retired on October 5 because the exact source report was absent. Their backup
is indexed in the [report catalog](reports/README.md).

## Acceptance measured October 3

Direct Codex review of fixed app questions: **30/30 supported, 5/5 declines,
5/5 follow-ups** daily; **56/56 supported, 8/8 declines** broader. Three repeats
of 13 fragile cases also pass. These finite results are not owner-use validation
or calibrated judge scores. See the [accepted report catalog](reports/README.md).

October 2's corrected-prefix and model experiments are historical rejected
candidates. Their results do not describe the accepted implementation. The
report catalog retains the key acceptance evidence and its dependencies;
superseded experiments are archived after a hash-verified local backup.
Reports use `reports/YYYY-MM-DD/run-name/artifact.json`. Their unchanged raw
contents keep historical paths; the [identity manifest](reports/manifest.json)
maps those paths to the current locations. The three dated fixtures above were
moved out of reports without changing their bytes or hashes. Stable reusable
fixtures keep their existing paths.

## Run retrieval checks

From the repository root with Qdrant and Ollama running, provide a question
set and its matching source directories in a separate collection. `--golden`
is required:

```bash
DOCUMENTS_PATHS=/absolute/path/to/evaluation-documents \
  QDRANT_COLLECTION=documents_chunks_evaluation \
  go run ./cmd/evaluator --golden /absolute/path/to/questions.json \
  --no-rerank --runs 3 --report .local/evaluation/retrieval.json
```

Default evaluator output is `.local/evaluation/<unix_ts>.json`. Use
`--ensure-ingest` only when an import of the configured sources is intended;
unchanged-file skipping does not upgrade old chunks after an indexing-policy
change. For a full reindex, use a copied configuration with unused document/cache
collection names and all source originals, preserving old collections and
configuration for rollback.

## API performance and usefulness review

The saved-answer and user-path runners were retired October 5. Use the
[Locust suite](../../benchmark/README.md) for API performance: retrieval, chat,
mixed traffic, cache reuse, follow-ups and uploads. It records timings without
saving complete answers. Use a separate evaluation API for upload runs because
uploaded documents remain indexed.

For usefulness review, save questions for chosen indexed documents and inspect
dashboard answers and citations. Check whether the actual cited passage
supports each claim. Keep document excerpts in ignored local review records;
use the Go evaluator above for repeatable retrieval/answer-quality checks.

## Fixture maintenance and optional research

The two generators tied to the removed sample corpus have been retired.
Preserve historical fixture bytes and author new inputs for a new corpus.
Do not change a fixture to make a failing implementation pass. Record changed
source/fixture hashes and evaluate a new baseline when inputs intentionally
change.

See the [evaluator guide](../../internal/eval/README.md) for generation-judge
configuration and release-gate validation. Automated judge scores remain
uncalibrated. The optional public-math importer was retired with its absent
corpus and candidate pack.
