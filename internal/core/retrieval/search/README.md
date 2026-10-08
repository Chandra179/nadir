# Retrieval search

Owns query validation, fragment embedding, hybrid document search, RRF-backed
candidate handling, optional calibrated fusion, confidence-gated optional
reranking, diversity caps, and semantic-cache lookup. Storage and provider
details arrive through narrow injected seams.

Each question is embedded once, in one batch of its fragments. The first
fragment is the whole question; its vector is also the semantic-cache key and
the vector stored with a cache write, so a miss makes one model call and a hit
makes one. The cache answers a request when the search that produced the entry
asked for at least `top_k` results, even if the corpus or the per-file cap
returned fewer.

The per-file cap (`search.max_chunks_per_file`) runs after ranking. Each store
leg is asked for `capOverfetchMul` times the needed candidates so the cap
backfills from lower ranks instead of shrinking the result; the reranker's
candidate budget is unchanged.

Embedding task prefixes apply to a separate dense-input slice. Hybrid lexical
search and optional fusion scoring receive the original query fragments, so
embedding instructions cannot become document search terms.

The default path keeps Qdrant's single-request RRF result for low resource use.
The opt-in `search.fusion` path asks the Qdrant Adapter for dense and BM25 leg
rankings, then applies weighted rank-RRF in this module. Raw dense and sparse
scores are never mixed. It can add deterministic exact-phrase and header-overlap
boosts, with query-type profiles and minimum-overlap thresholds. Public API
requests use the bounded query classifier; internal callers may supply a query type.

When adaptive reranking is enabled, the Qdrant Adapter returns the fused RRF
ranking plus dense and lexical leg rankings in one batch request. Search keeps
the fused result when both legs agree on their top candidate and the fused
top-result margin is strong; it calls the cross-encoder when the legs disagree
or the margin is weak. The decision and dependency cost are recorded in the
Retrieval result for evaluation and operations.

Change here for ranking, filtering, top-k, or query orchestration.
Search owns its candidate and filter values; `providers/` translate them to
provider protocols. Verify with
`go test -race ./internal/core/retrieval/search` and the evaluator.

`interface.go` exposes only `Retriever`; storage and optional reranking/batch
seams are private in `private_interfaces.go`.
