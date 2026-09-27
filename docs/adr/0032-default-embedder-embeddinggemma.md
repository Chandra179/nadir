# 0032 — Default embedder is EmbeddingGemma-300m

- **Status:** Accepted
- **Date:** 2026-09-27
- **Deciders:** Chandra

## Context

The default dense embedder was `nomic-embed-text`. Two isolated comparisons
over the 133-query fixture, each with a full reindex of both arms, measured
EmbeddingGemma-300m far ahead of it:

- 2026-09-14 experiment: HitRate@5 0.932 vs 0.789, MRR@10 0.735 vs 0.637.
  The default change was deferred: higher query latency and no
  release-gated evidence.
- 2026-09-27, on the current build, with same-session controls: golden
  4-document corpus HitRate@5 **0.955** vs 0.797, MRR@10 0.742 vs 0.641;
  full 14-document corpus **0.940** vs 0.759, MRR@10 0.795 vs 0.641 —
  with natural distractors present. Embed latency 95/106 ms p50/p95 versus
  ~25/35 ms for Nomic.

The retrieval-miss pool that capped answer quality (26 of 133 queries)
converted almost entirely under EmbeddingGemma (~21 hits gained). The
generation gate that depends on retrieval remains open for an independent
reason (the answer-model bracket documented in TODO), but the retrieval
ceiling itself was the embedder.

## Decision

The default embedder becomes `embeddinggemma-300m-q8:latest` at 768
dimensions, with the EmbeddingGemma task-instruction prefixes:
queries `task: search result | query: ` and documents
`title: none | text: `.

## Consequences

- Changing the embedder changes every dense vector: upgrading an existing
  deployment requires a full reindex. Chunk IDs stay stable, so versioned
  replacement handles the swap document by document, but mixed
  old/new-vector states must be avoided — reindex the whole corpus.
- The semantic cache holds query vectors from the previous embedding space;
  the destructive reset path invalidates it (generation bump plus backend
  clear), so reset-and-reindex is the safe upgrade sequence.
- Query latency roughly triples (still ~100 ms locally); accepted for
  portable single-node use.
- HyPE removal (2026-09-27) reflected the same ingest-cost discipline; the
  embedder swap costs no ingest-time LLM calls — it only changes which
  model embeds.
- The generation-quality gate stays open: no measured configuration passes
  both its bars yet; the binding constraint is documented in TODO as the
  answer model, not retrieval.
