# Retrieval search

Owns query validation, fragment embedding, hybrid document search, RRF-backed
candidate handling, optional calibrated fusion, confidence-gated optional
reranking, diversity caps, and semantic-cache lookup. Storage and provider
details arrive through narrow injected seams.

The default path keeps Qdrant's single-request RRF result for low resource use.
The opt-in `search.fusion` path asks the Qdrant Adapter for dense and BM25 leg
rankings, then applies weighted rank-RRF in this module. Raw dense and sparse
scores are never mixed. It can add deterministic exact-phrase and header-overlap
boosts, with query-type profiles and minimum-overlap thresholds. Evaluation
passes the golden query type; live callers may omit it and use the bounded
classifier.

When adaptive reranking is enabled, the Qdrant Adapter returns the fused RRF
ranking plus dense and lexical leg rankings in one batch request. Search keeps
the fused result when both legs agree on their top candidate and the fused
top-result margin is strong; it calls the cross-encoder when the legs disagree
or the margin is weak. The decision and dependency cost are recorded in the
Retrieval result for evaluation and operations.

Change here for ranking, filtering, top-k, or query orchestration. Use
Search owns its candidate and filter values; `adapters/` translate them to
provider protocols. Verify with
`go test -race ./internal/retrieval/search` and the evaluator.

`interface.go` exposes only `Retriever`; storage and optional reranking/batch
seams are private in `private_interfaces.go`.
