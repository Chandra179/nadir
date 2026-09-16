# 0028 — Annotated Retrieval golden set

- **Status:** Accepted
- **Date:** 2026-09-13
- **Deciders:** Chandra, Codex
- **Supersedes:** [0018](0018-repeatable-retrieval-evaluation.md) for fixture shape only

## Context

The repeatable evaluator measured a 34-query sample fixture, which was too
small and too uniform for ranking decisions. It had no explicit representation
for close distractors, multi-hop evidence, or the claims a generated answer
must satisfy. Retrieval and generation quality therefore could not be
reviewed from one controlled dataset.

## Decision

Use schema version 3 for the active golden fixture. Every annotated query
declares a query type, expected answer, required claims, and a
generation-faithfulness label. Queries may also declare evidence distractors;
the evaluator reports the fraction of queries where a distractor enters the
top-k result set. Relevant evidence remains the source of Retrieval metrics.

Schema version 3 also records a deterministic corpus manifest, privacy-review
metadata, separate per-query relevance judgments, and the adjudication method
used to produce canonical labels. Annotator records explicitly distinguish
human independent reviewers from synthetic evaluator passes.

The committed fixture is 133 expert-authored synthetic user-intent queries
over the sample corpus. Top-level metadata records its provenance, exact
corpus manifest, and makes clear that it contains no production user data. Two
synthetic judgment passes exercise the multi-judgment contract, but are not
independent human review. It is a development regression fixture, not a
substitute for consent-safe production query collection or independent expert
review.

A separate public-data path uses ARQMath Task 1 as a review-pack candidate. It
selects 40 topics from each 2020–2022 edition, records pinned source hashes,
and preserves a fixed answer candidate pool. The importer can normalize the
licensed Posts snapshot, but it cannot supply the required two genuine human
reviews or privacy/legal approval. Therefore the candidate remains outside the
active release gate until those external artifacts are present.

## Consequences

- Ranking experiments cover direct answers, formulas, procedures, comparisons,
  and multi-hop evidence instead of only single-anchor fact questions.
- Required claims and expected answers are ready for a future judge-model
  generation evaluator without coupling Retrieval to a model provider.
- Historical reports remain valid as 34-query measurements, but they must not
  be compared with new reports as if the datasets were identical.
- A production release gate still requires real user queries, expert labels,
  and generation-side evaluation.
