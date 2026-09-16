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
instead of becoming a misleading zero score. Each per-query record also stores
the Retrieval first-hit rank and relevant count, context chunk/token counts,
truncation status, answer size/status, judge status, failure class, and a
diagnostic cause. The report stores scores and latency, but not generated
answers or source text.

Answer generation sends deterministic Ollama options with the configured
`generator.max_output_tokens` as `num_predict` (512 in the shipped config).
The judge uses a bounded 128-token response, temperature zero, and a JSON
schema requiring all four numeric scores in `[0,1]`. The parser remains strict:
malformed, incomplete, or out-of-range output is a judge failure.

## Change here when

- Golden-set matching, annotation schema, ranking metrics, report shape, or
  repeatability changes.
- Evaluation needs a new quality signal or a new command option.

Change `retrieval/search/` when production ranking behaviour changes. Change
`test/evaluation/` for golden queries and checked-in reports. Schema version 3
queries include everything from schema version 2 plus query tags, two recorded
relevance judgments, and adjudication metadata. The set metadata identifies
the corpus manifest, privacy review, and annotator records. Do not put
evaluation-only shortcuts into production Retrieval or use cached answers as
quality evidence.

The committed fixture is currently a schema-v3 pack of 133 expert-authored
synthetic user-intent queries over `samples/`. It covers factoid, formula,
procedure, comparison, multi-hop, ambiguous, negative, and distractor cases.
Its metadata records the exact four-document manifest, the absence of
production user data, and two separately recorded synthetic evaluator passes.
Those passes are explicitly not independent human judgments. The canonical
`relevant` and `distractors` fields are the adjudicated regression labels.
The fixture is intentionally separate from the production-query collection
process: real queries require consent-safe sampling, privacy approval,
independent expert relevance judgments, and answer-faithfulness labels before
they can serve as a release gate.

The evaluator keeps that boundary explicit. Use `go run ./cmd/evaluator
--require-release-gate --golden path/to/production-golden.json` only with a
schema-v3 set whose metadata identifies a consented, non-synthetic production
dataset, records its provenance, representative corpus manifest, approved
privacy review, independent human annotators, per-query judgments, and sets
`release_gate: true`. To review a candidate fixture before starting any
external dependency, run `go run ./cmd/evaluator --validate-only
--require-release-gate --golden path/to/production-golden.json`. The repository
cannot create or approve that production evidence on its own.

The repository includes a pending public ARQMath Task 1 candidate pack under
`test/evaluation/arqmath/`. It deterministically selects 40 topics from each
the 2020, 2021, and 2022 editions and retains each source qrel candidate pool.
`scripts/import_arqmath.py` verifies the pinned public artifacts and, when the
large Posts snapshot is supplied with a trusted SHA-256, normalizes answer
posts into a manifest-backed corpus. The candidate is not a release gate until
two real independent math-capable reviewers, adjudication, and privacy/legal
approval are present.

The captured generation baseline was triaged in
[`generation-triage-20260916.json`](../../test/evaluation/reports/generation-triage-20260916.json).
The implementation fixes header-only evidence, bounds answer/judge output,
and records diagnostic causes. A live post-change generation rerun remains
pending until Qdrant and Ollama are available; the baseline must not be
overwritten or presented as post-change evidence.

Generation evaluation requires explicit judge configuration and an operator
confirmation that the judge is larger than `generator.model`; there is no
endpoint or model fallback:

```bash
go run ./cmd/evaluator --no-rerank --runs 1 --generation-eval \
  --judge-addr http://localhost:11434 \
  --judge-model phi4-mini:latest --judge-is-larger \
  --report test/evaluation/reports/generation-local.json
```

The committed 133-query candidate is useful for engineering regression checks,
but its synthetic provenance and non-human annotators prevent it from being a
production release gate. Use a consent-safe, expert-judged fixture and record
the answer-model and judge-model hardware for release decisions.

## Verification

Run `go test ./internal/evaluation` for metric and harness changes, then run
`go run ./cmd/evaluator --runs 3` against the configured corpus when validating
real ranking quality. Use `--require-release-gate` only after the production
query collection has passed consent, expert-judgment, and faithfulness review.
