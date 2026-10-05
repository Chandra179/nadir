# Retrieval and generation evaluation

Development-only measurement used by `cmd/evaluator`. The harness shares the
production Retrieval and prompt code, bypasses semantic cache, and records
Hit Rate, Recall, MRR, nDCG, distractor hits and pooled latency across repeated
runs. The 133-query golden fixture and 64-query representative fixture are
synthetic regression inputs, not production release gates.

With `--generation-eval`, the harness uses the production prompt, narrow literal
comparison path and citation correction. It records the answer method, answer,
exact admitted context, citations, raw judge response, scores and failures.
Reports therefore contain source excerpts: use ignored local output for owner
notes and review privacy before publishing evidence.

The separately configured judge returns faithfulness, answer relevancy, context
precision and context recall in `[0,1]`. Its response is bounded to 128 tokens,
temperature zero and a required JSON schema. Malformed, incomplete and
out-of-range output is a judge failure. Answer generation uses the configured
output/context limits and temperature zero. Model size alone does not establish
judge suitability or calibration; `--judge-suitability` records the operator's
reasoning and limits. There is no judge endpoint/model fallback.

## Usage

Run from the repository root against running Qdrant and Ollama. The default
reading configuration has no source paths. Supply a query set and matching
source directories explicitly, using a separate evaluation collection:

```bash
DOCUMENTS_PATHS=/absolute/path/to/evaluation-documents \
  QDRANT_COLLECTION=documents_chunks_evaluation \
  go run ./cmd/evaluator --golden /absolute/path/to/questions.json \
  --no-rerank --runs 3 --report .local/evaluation/retrieval.json
```

Default output is `.local/evaluation/<unix_ts>.json`, ignored by Git. Use
`--report` to select another path. Promote reviewed evidence into
`test/evaluation/reports/YYYY-MM-DD/run-name/artifact.json` together with its
fixture/config/model provenance, registration, comparison and limitations.
Record its original identity, hash and retention reason in the manifest; see the
[retention policy](../../test/evaluation/reports/README.md).

For generation evaluation, choose an already installed judge and explicitly
record why it is suitable. This diagnostic example uses the previously observed
Phi-4-mini judge; it is not independently calibrated:

```bash
DOCUMENTS_PATHS=/absolute/path/to/evaluation-documents \
  QDRANT_COLLECTION=documents_chunks_evaluation \
  go run ./cmd/evaluator --golden /absolute/path/to/questions.json \
  --no-rerank --runs 1 --generation-eval \
  --judge-addr http://localhost:11434 --judge-model phi4-mini:latest \
  --judge-suitability 'Diagnostic only; judge is not independently calibrated' \
  --report .local/evaluation/generation-local.json
```

Acceptance uses actual answer/source review. Results measured October 3 and
required older comparisons are indexed in the
[report catalog](../../test/evaluation/reports/README.md).
The sample corpus has been removed. Committed synthetic fixtures retain their
historical source identities for schema checks and interpreting dated reports;
live runs need an available matching corpus. The September 30 calibration
packet was retired with its missing source report. No independent judge
calibration is claimed.

## Release and research boundaries

`--require-release-gate` accepts only schema-v3 consented, non-synthetic
production evidence with approved privacy review, representative corpus,
independent human judgments and adjudication, and `release_gate: true`.
Validate a supplied fixture without external services:

```bash
go run ./cmd/evaluator --validate-only --require-release-gate \
  --golden path/to/production-golden.json
```

The committed synthetic fixtures remain `release_gate: false`. Their separate
synthetic annotator passes are not independent human judgments. The public-math
importer was retired because its corpus and candidate pack were absent.
API performance tests use the [Locust suite](../../benchmark/README.md).

## Change and verification

Change this package for metric definitions, fixture validation, report shape
and harness behavior. Change `internal/core/retrieval/search/` for production
ranking and `test/evaluation/` for regression fixtures. Avoid evaluation-only
shortcuts in production Retrieval.

Run `go test ./internal/eval ./cmd/evaluator` for harness changes. Run the live
retrieval packs when ranking changes require measured evidence. Do not overwrite
historical reports or use a different fixture/corpus as a controlled baseline.
