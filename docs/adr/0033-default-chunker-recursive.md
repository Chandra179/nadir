# 0033 — Default chunker stays recursive

- **Status:** Accepted
- **Date:** 2026-09-27
- **Deciders:** Chandra

## Context

The backlog required benchmarking recursive versus sentence-window chunking
on the release-gated fixture before any change to the chunker default. The
sentence-window provider is fully implemented: it embeds each extracted
segment with its file/header context and stores a ±3-sentence window in the
`window_text` payload, which is what the generator, the answer API, and the
BM25 fusion leg consume at query time.

The comparison was pre-registered before any measurement: on the golden
4-document corpus isolation, with the EmbeddingGemma default embedder, no
reranker, and a full reindex of both arms, sentence-window would become the
default only if it gained ≥ 0.015 HitRate@5, regressed no more than 0.01 on
nDCG@5 and MRR@10, and kept point count within 8×, ingest wall time within
8×, and query p50 within 2× of recursive. A second confirmation stage
(rerank and generation evals on both arms) would only run if the primary
bars passed.

Both arms were measured in the same session on 2026-09-27:

| Metric | Recursive (default) | Sentence-window | Bar for sentence-window |
|---|---|---|---|
| HitRate@5 | **0.940** | 0.925 | ≥ 0.955 — **failed** |
| Recall@5 | **0.930** | 0.909 | — |
| MRR@10 | 0.737 | **0.743** | ≥ 0.727 — passed |
| nDCG@5 | 0.779 | 0.779 | ≥ 0.769 — passed |
| Distractor@5 | 0.271 | **0.256** | — |
| Query p50/p95 | 96.2/109.4 ms | 97.1/109.9 ms | ≤ 2× p50 — passed |
| Qdrant points | 29 | 35 | ≤ 8× — passed |
| Ingest wall time | 2.79 s | 0.97 s | ≤ 8× — passed |

A material finding: the sentence-window segmenter degenerates on this
corpus. The sentence pattern requires punctuation followed by whitespace,
and bullet-list markdown ends lines without punctuation, so whole sections
collapse into a few coarse segments (35 points rather than the hundreds of
true sentences), and the shared plain-text extraction fuses adjacent list
items without separators (for example "…Newton's methodDoes not require…").
That extraction layer is common to both arms, so the comparison is fair, but
it means the arm measured is "window-context at query time with
section-level segments," not true per-sentence embedding.

## Decision

Recursive stays the default chunker. Sentence-window failed the primary
HitRate@5 bar (0.925, 0.015 below the required 0.955 and below the control);
ranking quality among hits held, but the pool of retrieved hits shrank. Per
the pre-registration, the confirmation stage was not run and the default is
unchanged.

## Consequences

- No reindex is required for existing deployments; the decision changes
  nothing operational.
- The measured implementation's segmenter is the bottleneck, not the
  window mechanism (which worked: `window_text` was populated, returned to
  the generator, and slightly reduced distractor rates). Any revisit
  requires a markdown- and formula-aware segmenter as a pre-condition,
  followed by a new pre-registered comparison.
- Changing the chunker changes chunk identity (chunk IDs bind to chunk
  index and line start), so any future switch requires a full reindex —
  versioned replacement handles it, and the reset path invalidates the
  semantic cache.
- Reports: [recursive](../../test/evaluation/reports/chunk-recursive-goldencorpus-20260927.json),
  [sentence-window](../../test/evaluation/reports/chunk-sentencewindow-goldencorpus-20260927.json).
