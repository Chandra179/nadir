# RAG

Nadir answers questions from a collection of local documents. You point it at a directory of markdown files, PDFs, or plain text. It ingests them, builds a search index, and answers questions in natural language. You interact through an HTTP API, POST a query, get back ranked passages or a generated answer with citations.
**Repo**: https://github.com/Chandra179/nadir

## Architecture

1. **Ingest & Chunk.** Sentence-chunking splits on sentence boundaries for precise citations. A configurable strategy selects chunk size and overlap, ex: split chunk on sentence. split per semantic.

2. **Embed.** Each chunk gets prefixed with its file path and heading before embedding. This contextual prefix anchors the vector in document structure without changing the stored text the same chunk in a different context produces a different embedding.

3. **Semantic Cache.** Before search, the query embedding checks cosine similarity against a dedicated Qdrant collection. Above a configurable threshold, cached results return immediately. On a miss, the pipeline writes the result back asynchronously. The cache only activates when the client does not request generation.

4. **Search.** Two legs run in parallel. Dense search finds nearest neighbors by cosine similarity. Sparse search runs BM25 over the full-text index, then rescales scores using SPLADE. Results fuse by Reciprocal Rank Fusion (RRF) — each candidate's rank from each leg gets a reciprocal score, summed across legs. 
   Long queries split on sentence boundaries first; each fragment searches independently, then results deduplicate and re-sort. This avoids dilution from embedding a long, multi-topic query into a single vector and catches out-of-domain queries that pure dense would miss.

   **Metadata filtering.** `POST /search` accepts an optional `"filter"` object with `file_path`, `header`, and `source_sha` fields. When set, only chunks matching those exact keyword values are considered for search — useful for scoping queries to specific files or sections.

5. **Re-rank.** The top N candidates are re-scored by the cross-encoder reranker sidecar, if enabled and available.

6. **Generate.** When generation is requested, chunks are reordered by the "lost in the middle" heuristic — most relevant at the start and end of the context window, least relevant in the middle — following the empirical finding that LLMs use information at both ends far better than the middle. Chunks are placed into a system prompt within the token budget, and Ollama streams the answer token by token back to the client.

## Key Tradeoffs

**Hybrid search vs pure dense.** Hybrid catches queries that use different vocabulary than the documents. Pure dense is faster and simpler but misses relevant results when terminology diverges. For out-of-domain queries, hybrid wins. For well-matched vocab, pure dense performs similarly at lower latency.

**Contextual prefix vs late interaction.** Prefixing the file path and heading into the embedding anchors meaning cheaply. Late-interaction models (ColBERT) can match more flexibly at search time, but require a different retrieval architecture and more memory.