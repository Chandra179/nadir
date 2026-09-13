# Retrieval search

Owns query validation, fragment embedding, hybrid document search, RRF-backed
candidate handling, optional reranking, diversity caps, and semantic-cache
lookup. Storage and provider details arrive through narrow injected seams.

Change here for ranking, filtering, top-k, or query orchestration. Use
Search owns its candidate and filter values; `adapters/` translate them to
provider protocols. Verify with
`go test -race ./internal/retrieval/search` and the evaluator.

`interface.go` exposes only `Retriever`; storage and optional reranking/batch
seams are private in `private_interfaces.go`.
