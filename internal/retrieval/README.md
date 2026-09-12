# Retrieval context

Retrieval turns a user query into ranked, storage-independent context. It owns
optional conversational rewriting, query fragmentation, hybrid dense/BM25
search, RRF, semantic cache lookup, reranking, and final context selection.

| Task | Start here | Related |
|---|---|---|
| Query orchestration, fragments, RRF, reranking, top-k | `search/` | Qdrant documents, reranker |
| Cache hit/miss, invalidation, versioning | `cache/` | indexing, Qdrant shared |
| Follow-up rewrite contract | `rewriting/` | Conversation Chat, Ollama rewriter |

Change a child when the concept already exists. Add a new child only for a new
retrieval stage with a meaningful seam and independent tests. Qdrant payloads
stay in its Adapter; generation and prompt construction stay in Conversation;
quality claims are measured in `evaluation/`.

## Verification

Run `go test -race ./internal/retrieval/...`. Use the evaluator for ranking or
latency changes and compare cache-on and cache-bypassed behaviour separately.
