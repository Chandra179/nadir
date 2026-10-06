# 0034 — Chunker correctness fixes, chunk size stays 512, answer model moves to gemma3:4b

- **Status:** Accepted
- **Date:** 2026-09-29
- **Deciders:** Chandra

## Context

An external audit (papers, practitioner consensus, production RAG codebases)
and a fresh evaluator session surfaced correctness bugs in the recursive
chunker and the retrieval result-identity path, all invisible to earlier
evaluations because the golden four-document fixture contains neither fenced
code blocks nor oversized paragraphs:

1. `extractSections` captured only Paragraph/List/Blockquote nodes, so
   **top-level fenced code blocks were silently dropped** from section text.
   Six of the fourteen full-corpus documents contain fences (up to 36 in
   `uber-architecture.md`); all of that content was unindexed.
2. `nodeToPlainText` fused adjacent list items without a separator
   ("Newton's methodDoes not require…"), corrupting chunk text and BM25 term
   boundaries. This flaw is documented in ADR 0033 as a shared-extraction
   finding; it is now fixed at the source.
3. `mergeSplits` accepted a single part longer than `chunk_size` whole
   (`current == ""` branch), emitting oversized chunks that Ollama then
   truncated silently at embed time. The fix mirrors LangChain's
   RecursiveCharacterTextSplitter: oversized parts are re-split with the
   remaining finer separators and a rune-wise `hardSplit` bounds the result.
4. `SearchCandidate.Key()` omitted `ChunkIndex`, and chunks of one section
   share the section's `LineStart`, so result dedup collapsed every section to
   one retrievable chunk.
5. Multi-sentence queries were searched per sentence fragment only; the full
   composite query was never searched itself.

All five were fixed with unit tests, including a property test that no
emitted chunk exceeds `chunk_size` + overlap + separator, and a regression
test that two chunks of one section both survive `multiSearch`. An embed-time
guard (`embedder.max_input_chars`, default 8000, env `EMBEDDER_MAX_INPUT_CHARS`)
now rune-clamps dense embed inputs explicitly; the BM25 leg keeps full text.

The same session re-measured the reranker on the current EmbeddingGemma
default (never re-validated after the ADR 0032 swap): with rerank
HitRate@5 0.910 / nDCG@5 0.767 at 511 ms p50; without rerank 0.947 / 0.827 at
103 ms. The reranker is now net-negative at the first stage's ceiling —
consistent with the undiscussed distractor regression in
`generation-gemma-rerank-promptfix-20260927` (0.466 vs 0.271).

## Chunk-size A/B (pre-registered)

Question: raise `chunk_size` from 512 runes (~110–140 tokens, well below the
~512-token production modal default) to 2048 runes (~500 tokens)? Bars to
adopt 2048, fixed chunker both arms, full corpus, no reranker: HitRate@5 ≥
control − 0.01; nDCG@5 and MRR@10 not worse by > 0.01.

| Metric | 512 (fixed), 2 runs | 2048, 2 runs | Verdict |
|---|---|---|---|
| HitRate@5 | 0.970 / 0.985 | 0.977 / 0.977 | parity |
| Recall@5 | 0.949 / 0.970 | 0.950 / 0.950 | parity |
| MRR@10 | 0.806 / 0.809 | 0.788 / 0.788 | −0.020 — **failed** |
| nDCG@5 | 0.831 / 0.840 | 0.820 / 0.818 | −0.017 — **failed** |
| Distractor@5 | 0.203 / 0.211 | 0.195 / 0.188 | slight win |
| Qdrant points | 442 | 220 | — |

## Decision

1. The five correctness fixes are adopted; the fixed chunker is the new
   baseline. On the same-session control the fixes lifted the full-corpus
   retrieval metrics from the 2026-09-27 stored 0.940/0.795/0.823
   (Hit@5/MRR@10/nDCG@5) and the same-day pre-fix re-run 0.947/0.796/0.827 to
   **0.970–0.985 Hit@5, 0.806–0.809 MRR@10, 0.831–0.840 nDCG@5** — every
   pre-registered gate passed (Hit@5 ≥ 0.937, ranking within 0.01).
2. `chunk_size` stays **512 runes**. The 2048 arm failed both ranking bars;
   HitRate parity with distractor improvement is not enough. Run-to-run
   HitRate variance (±0.015) is noted for future pre-registrations.
3. The reranker re-measurement is recorded as standing evidence: on the
   EmbeddingGemma default the cross-encoder degrades every retrieval metric
   at ~5× latency. `reranker.enabled` remains configurable; the adaptive
   gate is the recommended path for any re-enablement and a follow-up
   pre-registration should set its margin from this data.

## Answer-model benchmark (generation gate)

With the fixes above (num_ctx pinned at 4096, abstention instruction
tightened to "say so and stop", judge auditability in reports), the
pre-registered generation gate — faithfulness ≥ 0.65 **and** answer
relevancy ≥ 0.75, 133 golden queries, phi4-mini judge — was run on both
candidates:

| Metric | gemma3:1b + fixes | gemma3:4b + fixes |
|---|---|---|
| Faithfulness | 0.750 | **0.881** |
| Answer relevancy | 0.756 | **0.803** |
| Context precision / recall | 0.565 / 0.735 | 0.480 / 0.755 |
| Evaluated / failures | 133 / 0 | 133 / 0 |
| Diagnostic causes | 9 retrieval, 21 context, 20 generation | 8 retrieval, 27 context, 3 generation |
| Answer p50/p95 | 476/843 ms | 4309/4953 ms |
| Judge p50 | 2179 ms | 4595 ms |

Both arms now pass the gate that no pre-fix configuration could pass
simultaneously (best prior runs: 0.729/0.682 or 0.632/0.757); the fixes alone
were worth +0.02..+0.12 faithfulness on the same 1B model, and retrieval-side
diagnostic misses fell from 31 to 9. gemma3:4b passes with real margin
(relevancy +0.05 over the bar, faithfulness +0.23) and collapses generation
failures to 3, at ~9× answer latency on this hardware.

**Decision:** `generator.model` default moves to `gemma3:4b` per the
pre-registration (4b passes the gate → flip). gemma3:1b remains the
documented low-latency fallback and still passes the gate post-fixes.
These are September 29 measurements with an uncalibrated automatic judge.
The raw 1B/4B reports were archived on October 5; the dated results and model
decision remain above. Following the October 6 cleanup, see the
[evaluator recovery guide](../../eval/README.md#retired-assets-and-recovery)
for Git history and local backups of the former evaluation assets.

## Consequences

- Chunk text changed → full reindex required once (performed 2026-09-29 via
  reset + re-ingest; point count 368 → 442 with code content indexed).
- The golden `contains` matching is content-based, so no fixture change was
  needed; retrieval gates passed unchanged.
- September 29's two 512 runs, two 2048 runs, pre-fix baseline and reranker
  comparison are archived with their original hashes. The tables above retain
  the historical comparison; October 3 acceptance is a separate measurement.
- Operational note: the CUDA reranker profile (~3.7 GiB VRAM) and Ollama
  LLM serving do not fit together on a 6 GiB GPU — phi4-mini judge calls
  fail with `cudaMalloc failed: out of memory` while the sidecar holds the
  device. Generation evaluation must run with the sidecar stopped or on the
  CPU profile.
