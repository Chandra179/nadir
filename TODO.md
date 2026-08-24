# TODO

Retrieval-accuracy roadmap (audit + research cross-reference, Aug 2026). Each
change is measured with `cmd/evalbench` (HitRate@k / Recall@k / MRR@10 /
nDCG@10) before and after.

## Measured results (34-query golden set over `samples/`, top_k=5)

| Stage | HitRate@5 | MRR@10 | nDCG@5 | p50 latency |
|---|---|---|---|---|
| Baseline (MiniLM reranker, un-prefixed index) | 0.824 | 0.689 | 0.742 | 201ms |
| + nomic prefixes + BM25 contextual align | 0.853 | 0.707 | 0.762 | 190ms |
| + bge-reranker-v2-m3 sidecar | **0.882** | **0.824** | **0.857** | 3206ms |
| same, no-rerank reference | — | 0.740 | 0.784 | 49ms |
| HyPE enabled on toy corpus | 0.882 | 0.804 | 0.823 | ~flat |

Reports in `tests/eval/reports/`. Rerank CPU latency (~3.2s p50) exceeds the
1–2s budget → quantized/base model swap listed under Phase 3.


## Phase 0 — Eval foundation ✅

- [x] `internal/eval` metrics (Recall@k, MRR@10, binary nDCG@10, HitRate@k, latency percentiles)
- [x] `internal/eval/harness.go` — golden-set runner + JSON reports (`tests/eval/reports/`)
- [x] `cmd/evalbench` CLI (`--ensure-ingest`, `--no-rerank`, `--runs`, `--report`)
- [x] Seed golden set: `tests/eval/golden.json` (34 queries over `samples/`)
- [ ] Grow golden set to 100+ queries as real corpus grows; keep distractor pairs

## Phase 1 — Cheap wins ✅

- [x] Swappable reranker model: `reranker.model` in config.yaml → `RERANKER_MODEL`
      env → sidecar. Default upgraded `ms-marco-MiniLM-L-6-v2` (~60 BEIR nDCG@10,
      can *hurt* vs no rerank per NVIDIA benchmark) → `BAAI/bge-reranker-v2-m3`
      (~71.5 BEIR, MIT). Revert via config; compose memory 1g→3g.
- [x] nomic task prefixes at call sites: `embedder.query_prefix` ("search_query: ")
      on query fragments, `embedder.document_prefix` ("search_document: ") on
      ingest embeds. Empty string disables (any-model swappable).
- [x] BM25 sparse leg now indexes the same contextual text the dense leg
      embeds (path > header + body), instead of bare chunk text.
- [x] **Requires one reindex** after pulling: drop collection, re-ingest
      (`curl -X DELETE localhost:6333/collections/documents_chunks`).

## Phase 2 — Index-time LLM enrichment ✅ (flags default OFF)

Zero query-time latency; one-time Ollama cost per chunk at ingest. Enabling
after a prior ingest requires a reindex.

- [x] `internal/enrichment` — Ollama chat client, lenient JSON parsing, graceful
      per-chunk degradation (warn + index without enrichment)
- [x] HyPE feature flag `enrichment.hype.enabled` (+ `questions_per_chunk`,
      default 3): hypothetical questions embedded as extra sibling points that
      carry the parent's identity fields → existing Key() dedup collapses them;
      DeleteByFile sweeps them; point IDs get `:hype:<n>` suffix
- [x] Contextual retrieval flag `enrichment.contextual.enabled`: LLM-written
      situational intro prepended before embedding/indexing
- [x] Env overrides `HYPE_ENABLED`, `CONTEXTUAL_ENABLED`
- [x] A/B on toy corpus: neutral/slightly negative (tiny clean corpus, no
      context fragmentation for it to fix; gemma3:1b question noise). Keep OFF
      until a real corpus exists; re-eval there — paper gains (+20pp precision)
      show on fragmented/larger corpora.

## Phase 3 — Optional / deferred

- [ ] Rerank latency: ONNX/int8 quantized bge-reranker-v2-m3 or BAAI/bge-reranker-base
      (current fp32 CPU p50 ≈ 3.2s > 1–2s budget; accuracy gain is +8.4pp MRR)
- [ ] Embedder swap (bge-m3 / snowflake-arctic-embed-l class, new dims +
      full reindex) — only if Phase 0 numbers show recall ceiling at nomic
- [ ] Adaptive similarity-threshold relaxation when retrieval comes back
      empty (arXiv 2507.16754)
- [ ] ONNX/int8 quantized reranker if bge-reranker-v2-m3 CPU latency hurts
- [ ] Wire docling sidecar into ingest (currently `.md` uploads only)

## Rejected (research says skip for this domain)

- HyDE / multi-query expansion — underperforms vanilla hybrid retrieval for
  precise numeric/entity queries (T2-RAGBench 2026); fragment-splitting
  already captures part of the benefit
- GraphRAG / RAPTOR — heavy machinery, unjustified at current corpus size
- Late chunking — efficiency win but sacrifices relevance/completeness vs
  contextual retrieval (arXiv 2504.19754); needs long-context pooling support
