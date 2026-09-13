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

The committed fixture is currently 109 curated queries over `samples/`. It is
intentionally separate from the production-query collection process: real
queries require consent-safe sampling, expert relevance judgments, and answer
faithfulness labels before they can serve as a release gate.

## Verification

Run `go test ./internal/evaluation` for metric and harness changes, then run
`go run ./cmd/evaluator --runs 3` against the configured corpus when validating
real ranking quality.
