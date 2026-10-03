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

Run from the repository root against running Qdrant and Ollama:

```bash
go run ./cmd/evaluator --no-rerank --runs 3
go run ./cmd/evaluator --golden test/evaluation/representative.json \
  --no-rerank --runs 3 --report .local/evaluation/representative.json
```

Default output is `.local/evaluation/<unix_ts>.json`, ignored by Git. Use
`--report` to select another path. Promote reviewed evidence into
`test/evaluation/reports/` together with its fixture/config/model provenance,
registration, comparison and limitations; see the
[retention policy](../../test/evaluation/reports/README.md).

For generation evaluation, choose an already installed judge and explicitly
record why it is suitable. This diagnostic example uses the previously observed
Phi-4-mini judge; it is not independently calibrated:

```bash
go run ./cmd/evaluator --no-rerank --runs 1 --generation-eval \
  --judge-addr http://localhost:11434 --judge-model phi4-mini:latest \
  --judge-suitability 'Diagnostic only; independent human calibration pending' \
  --report .local/evaluation/generation-local.json
```

The [local acceptance](../../docs/LOCAL_V1.md) uses actual answer/source review,
not automated judge averages. October 3's accepted results and older comparisons
are indexed in the [report catalog](../../test/evaluation/reports/README.md).
The September 30 [39-case blind calibration packet](../../test/evaluation/judge-calibration/20260930-phi4-mini/manifest.json)
remains awaiting independent human review and applies only to its exact source
report/model/fixture hashes.

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
synthetic annotator passes are not independent human judgments. The optional
`scripts/import_arqmath.py` builds public-math research inputs; its licensed
corpus and candidate pack are not present in this checkout and do not block
personal/local v1.

## Change and verification

Change this package for metric definitions, fixture validation, report shape
and harness behavior. Change `internal/core/retrieval/search/` for production
ranking and `test/evaluation/` for regression fixtures. Avoid evaluation-only
shortcuts in production Retrieval.

Run `go test ./internal/eval ./cmd/evaluator` for harness changes. Run the live
retrieval packs when ranking changes require measured evidence. Do not overwrite
historical reports or use a different fixture/corpus as a controlled baseline.
