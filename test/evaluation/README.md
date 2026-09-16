# Evaluation fixtures

The golden retrieval set and committed reports live beside this README. The
development evaluator loads them through `cmd/evaluator` and writes new reports
under this directory unless a different path is supplied.

Generation reports can be produced locally with a separately configured larger
judge model. They contain aggregate faithfulness, answer relevancy, context
precision, context recall, coverage, failures, and model-call latency, but do
not persist generated answers or retrieved source text. Per-query diagnostics
include paired Retrieval rank/counts, context size and truncation, answer and
judge status, failure class, and diagnostic cause.

The answer request uses temperature `0` and the configured
`generator.max_output_tokens` as Ollama `num_predict`; the judge uses a
temperature-zero 128-token JSON-schema response. Invalid, incomplete, and
out-of-range judge output remains a contract failure rather than being
silently repaired.

The active `golden.json` is a schema-v3 synthetic candidate pack with 133
queries over the four committed sample documents. It includes canonical
relevance labels, two separately recorded synthetic judgment passes, per-query
adjudication metadata, query-intent tags, and a deterministic corpus manifest.
The annotator passes are not independent human judgments and
`metadata.release_gate` remains `false`; this fixture is for regression and
E2E testing only.

Generate or refresh the candidate metadata with:

```bash
python3 scripts/generate_evaluation_candidate.py
```

Run it against local Qdrant and Ollama with:

```bash
go run ./cmd/evaluator --golden test/evaluation/golden.json \
  --no-rerank --runs 1 --ensure-ingest \
  --report test/evaluation/reports/e2e-generated-golden.json
```

The evaluator's `--require-release-gate` mode requires schema-v3 production
metadata, an approved privacy review, a representative corpus manifest, and
two verified independent human annotators for every query. The repository
cannot create those external approvals.

## Public ARQMath candidate

[`arqmath/`](arqmath/) contains a reproducible 120-query candidate selected
from ARQMath Task 1: 40 topics each from the 2020, 2021, and 2022 editions.
The importer verifies the pinned topic/qrels downloads, preserves source
query IDs and qrel candidate pools, and can normalize the full
`Posts.V1.3.zip` collection into stable Markdown documents. The raw collection
is not committed because of its size and non-commercial license terms.

```bash
python3 scripts/import_arqmath.py build \
  --source-dir var/evaluation/arqmath/source \
  --output-dir var/evaluation/arqmath/pack \
  --per-edition 40
```

The generated pack intentionally has `release_gate: false` until two genuine
independent math-capable reviewers label every fixed candidate pool, an
adjudicator records expected answers/claims/faithfulness, and privacy/legal
approval is recorded. See [`arqmath/README.md`](arqmath/README.md) for the
review-file format and merge command.

The captured generation baseline is triaged in
[`reports/generation-triage-20260916.json`](reports/generation-triage-20260916.json).
Its live post-change rerun is still pending unavailable Qdrant/Ollama services.
