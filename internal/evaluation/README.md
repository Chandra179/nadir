# Retrieval and generation evaluation

Development-only quality measurement for Retrieval and answer generation. The
evaluator loads a versioned golden set, bypasses semantic cache, passes each
query-type annotation into Retrieval, records final ranking and latency, and
reports Hit Rate, Recall, MRR, nDCG, distractor-hit rate, and percentiles. This
makes calibrated dense/BM25/RRF policies comparable without changing the
golden fixture or mixing provider score scales.

With `--generation-eval`, it also runs the production answer prompt once per
query and sends the answer, reference answer, required claims, and retrieved
context to a separately configured larger judge model. The judge returns four
scores in `[0,1]`: faithfulness, answer relevancy, context precision, and
context recall. Invalid judge output and model failures are recorded per query
instead of becoming a misleading zero score. The report stores scores and
latency, but not generated answers or source text.

## Change here when

- Golden-set matching, annotation schema, ranking metrics, report shape, or
  repeatability changes.
- Evaluation needs a new quality signal or a new command option.

Change `retrieval/search/` when production ranking behaviour changes. Change
`test/evaluation/` for golden queries and checked-in reports. Schema version 2
queries include a query type, expected answer, required claims, a
generation-faithfulness label, and optional distractor matches. Do not put
evaluation-only shortcuts into production Retrieval or use cached answers as
quality evidence.

The committed fixture is currently 133 expert-authored synthetic user-intent
queries over `samples/`. Its top-level metadata records provenance, the absence
of production user data, and the fact that the labels are single-expert
annotations. It is intentionally separate from the production-query collection
process: real queries require consent-safe sampling, expert relevance
judgments, and answer-faithfulness labels before they can serve as a release
gate.

The evaluator keeps that boundary explicit. Use `go run ./cmd/evaluator
--require-release-gate --golden path/to/production-golden.json` only with a
schema-v2 set whose metadata identifies a consented, non-synthetic production
dataset, records its provenance and expert judgment, and sets
`release_gate: true`. To review a candidate fixture before starting any
external dependency, run `go run ./cmd/evaluator --validate-only
--require-release-gate --golden path/to/production-golden.json`. The repository
cannot create or approve that production evidence on its own.

Generation evaluation requires explicit judge configuration and an operator
confirmation that the judge is larger than `generator.model`; there is no
endpoint or model fallback:

```bash
go run ./cmd/evaluator --no-rerank --runs 1 --generation-eval \
  --judge-addr http://localhost:11434 \
  --judge-model phi4-mini:latest --judge-is-larger \
  --report test/evaluation/reports/generation-local.json
```

The committed 133-query fixture is useful for engineering regression checks,
but its synthetic provenance prevents it from being a production release gate.
Use a consent-safe, expert-judged fixture and record the answer-model and
judge-model hardware for release decisions.

## Verification

Run `go test ./internal/evaluation` for metric and harness changes, then run
`go run ./cmd/evaluator --runs 3` against the configured corpus when validating
real ranking quality. Use `--require-release-gate` only after the production
query collection has passed consent, expert-judgment, and faithfulness review.
