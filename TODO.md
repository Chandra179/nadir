# TODO

Retrieval-accuracy roadmap (audit + research cross-reference, Aug 2026; items
re-audited and research pass re-verified Sep 2026). Each change is measured
with `cmd/evalbench` (HitRate@k / Recall@k / MRR@10 / nDCG@k) before and after.

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

- [x] `internal/eval` metrics (Recall@k, MRR, binary nDCG@k, HitRate@k, latency percentiles)
- [x] Golden-set runner + JSON reports (`tests/eval/reports/`): `Harness` in
      `internal/eval/dependencies.go`, `Run`/`LoadGoldenSet`/`WriteReport` in
      `internal/eval/service.go`
- [x] `cmd/evalbench` CLI (`--ensure-ingest`, `--no-rerank`, `--runs`, `--report`)
- [x] Seed golden set: `tests/eval/golden.json` (34 queries over `samples/`)
- [ ] Grow golden set to 100+ queries as real corpus grows; keep distractor pairs

## Phase 1 — Cheap wins ✅

- [x] Swappable reranker model: `reranker.model` in config.yaml → `RERANKER_MODEL`
      env → sidecar. Default upgraded `ms-marco-MiniLM-L-6-v2` (~60 BEIR nDCG@10,
      can *hurt* vs no rerank per NVIDIA benchmark) → `BAAI/bge-reranker-v2-m3`
      (~71.5 BEIR, MIT). Revert via config; compose memory 1g→3g.
      Caveat: the Go client never pushes `reranker.model` to the sidecar — the
      loaded model is agreed via the shared `RERANKER_MODEL` env/default only.
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

## Phase 3 — Measured gaps (next up)

- [ ] Rerank latency: quantize the existing sidecar before swapping models.
      `services/reranker` runs `sentence_transformers.CrossEncoder` fp32 on CPU
      (p50 ≈ 3.2s > 1–2s budget; the reranker buys +8.4pp MRR). Documented,
      proven path: ONNX dynamic int8 via Optimum
      (`CrossEncoder(..., backend="onnx")`) or OpenVINO qint8 —
      sentence-transformers' own CPU benchmarks recommend openvino-qint8 when
      small quality degradations are acceptable, onnx-O3 otherwise, with
      NanoBEIR nDCG@10 tracking fp32 within noise for most configs. A/B with
      evalbench; if quality drops, fall back to a quantized bge-reranker-base.
- [ ] Adaptive retrieval fallback for weak results (arXiv 2507.16754, "Never
      Come Up Empty": for novel queries, lowering the similarity bar beats
      returning nothing). Nadir adaptation: retrieval never filters by score
      today, so "empty" only happens on filter misses — grade result
      confidence (max fused/rerank score) and on weak results retry without
      the filter / with wider prefetch and per-file cap before answering
      "I don't know". Tradeoff: noise reaches the generator; the
      answer-only-from-context prompt is the guard.
- [ ] Conversational query rewriting: `chat.Ask` feeds the raw follow-up text
      to retrieval, so "what about the second one?" searches garbage. Rewrite
      follow-ups against the last N turns before search (Rewrite-Retrieve-Read,
      arXiv 2305.14283; proven as LangChain's condense-question pattern).
      Tradeoff: +1 LLM call per follow-up (cheap on gemma3:1b) and rewrite
      drift; skip when the session has no prior turns.
- [ ] Wire docling sidecar into ingest (currently `.md` uploads only; the
      sidecar is a standalone PDF→MD batch CLI with zero Go references). Make
      it an HTTP endpoint or a pre-ingest hook. Tradeoff: docling latency at
      ingest, and PDF structure doesn't map 1:1 onto the markdown
      `path > header` identity prefix — needs its own contextual-text rule.
- [ ] Per-stage observability: the `Metrics()` middleware is an empty stub
      (rfc.md open question). Record embed / search / rerank / generate
      durations + cache-hit rate so prod latency matches what evalbench sees;
      cheap and de-risks every item above.

## Phase 4 — Research candidates (paper → proven impl → tradeoff; measure first)

- [ ] CRAG-style retrieval self-correction (arXiv 2401.15884): LLM grades each
      candidate correct/incorrect/ambiguous; on failure rewrite + re-retrieve
      (no web-search fallback — local-first corpus). Proven impl: LangGraph
      `corrective-rag` template. Tradeoff: +1 judge call on the query-time
      path, misgrading risk with a 1B judge; reuses the rewriter from Phase 3.
- [ ] Generation-side eval with RAGAS (proven lib, LLM-as-judge): faithfulness,
      answer relevancy, context precision/recall. evalbench measures retrieval
      only — gemma3:1b answer quality is unmeasured. Needs a judge model bigger
      than the one under test (Ollama-hosted); LLM-judge noise and cost are the
      tradeoff. Retrieval metrics keep the binary ground truth.
- [ ] Speculative RAG (arXiv 2407.08223, Google): a small drafter generates
      answer drafts from partitioned chunk subsets; a larger verifier scores
      and picks in one pass — better accuracy at lower latency than one big
      generation. Needs two Ollama models (e.g. gemma3:1b draft + gemma3:4b
      verify). Tradeoff: more query-time compute and moving parts; the gain
      assumes the verifier is meaningfully better than the drafter.
- [ ] Adaptive-RAG query routing (arXiv 2403.14403, NAACL 2024): a classifier
      picks no-retrieval / single-step / iterative retrieval per query, so
      compute scales with question complexity. Proven impl:
      github.com/starsuzi/Adaptive-RAG, LangChain adaptive-rag template.
      Tradeoff: misrouting, an extra model, and iterative retrieval fights the
      "predictable one-shot" design (rfc.md). Defer until the golden set
      actually contains multi-hop queries.
- [ ] Qdrant quantization at scale (config-only, proven): scalar int8 ≈4×
      memory down with <1% quality loss; binary ≈32× down and up to 40× faster
      but requires rescoring + 2–4× oversampling (~0.98 recall). Not urgent —
      search is sub-100ms today and the reranker owns the latency budget;
      revisit when the corpus outgrows RAM. Tradeoff: oversampling feeds more
      candidates to the already-slow reranker.
- [ ] Embedder swap (bge-m3 / snowflake-arctic-embed-l class via Ollama, new
      dims + full reindex) — only if Phase 0 numbers show recall ceiling at nomic
- [ ] A/B sentence-window vs recursive chunker on the golden set (rfc.md open
      question; `window_size: 3`). Zero new code — pure harness run before any
      default change.

## Rejected (research says skip for this domain)

- HyDE / multi-query expansion — underperforms vanilla hybrid retrieval for
  precise numeric/entity queries (T2-RAGBench 2026); fragment-splitting
  already captures part of the benefit
- GraphRAG / RAPTOR — heavy machinery, unjustified at current corpus size
- Late chunking — efficiency win but sacrifices relevance/completeness vs
  contextual retrieval (arXiv 2504.19754); needs long-context pooling support
