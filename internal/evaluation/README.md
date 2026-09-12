# Retrieval evaluation

Development-only quality measurement for the Retrieval Module. The evaluator
loads a golden set, bypasses semantic cache, repeats each query, records final
ranking and latency, and reports Hit Rate, Recall, MRR, nDCG, and percentiles.

## Change here when

- Golden-set matching, ranking metrics, report shape, or repeatability changes.
- Evaluation needs a new quality signal or a new command option.

Change `retrieval/search/` when production ranking behaviour changes. Change
`test/evaluation/` for golden queries and checked-in reports. Do not put
evaluation-only shortcuts into production Retrieval or use cached answers as
quality evidence.

## Verification

Run `go test ./internal/evaluation` for metric and harness changes, then run
`go run ./cmd/evaluator --runs 3` against the configured corpus when validating
real ranking quality.
