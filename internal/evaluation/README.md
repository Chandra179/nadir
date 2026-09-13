# Retrieval evaluation

Development-only quality measurement for the Retrieval Module. The evaluator
loads a versioned golden set, bypasses semantic cache, repeats each query,
records final ranking and latency, and reports Hit Rate, Recall, MRR, nDCG,
distractor-hit rate, and percentiles.

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
dataset, records expert judgment, and sets `release_gate: true`. The repository
cannot create or approve that production evidence on its own.

## Verification

Run `go test ./internal/evaluation` for metric and harness changes, then run
`go run ./cmd/evaluator --runs 3` against the configured corpus when validating
real ranking quality. Use `--require-release-gate` only after the production
query collection has passed consent, expert-judgment, and faithfulness review.
