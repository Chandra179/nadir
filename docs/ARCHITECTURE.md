# Architecture

## Overview

Nadir is a semantic search engine for Markdown knowledge bases. It exposes two HTTP endpoints:

- `POST /ingest` — walk a directory, chunk and embed new/changed files, store in Qdrant
- `POST /search` — embed a query, run hybrid dense+sparse retrieval, return ranked chunks

All core logic lives in `internal/pkb/`. The HTTP layer (`internal/httpserver/`, `internal/middleware/`) is a thin wrapper. No `httpserver` or `middleware` imports flow inward — dependency direction is strictly inward toward `pkb`.

---

## End-to-End Pipelines

### Ingest Pipeline

```
LocalFileLister
  └── walks knowledge_base.path for .md files
  └── returns []FileEntry{Path, SHA}
        │
        ▼
IngestHandler.ServeHTTP
  └── Store.GetAllFileSHAs()       ← single paginated Qdrant scroll (bulk SHA check)
  └── for each file:
        SHA match? → skip
        SHA changed? → Store.DeleteByFile() then re-ingest
        New file? → ingest
              │
              ▼
        LocalFetcher.FetchFile()   ← reads raw .md from disk
              │
              ▼
        Pipeline.IngestFile()
          ├── Chunker.Chunk()
          │     RecursiveChunker: Goldmark AST walk
          │       heading → paragraph → sentence → word splits
          │       emits plain text (strips ##, **, _, fences, links)
          │     SentenceWindowChunker: index sentences,
          │       expand to ±N sentence window at retrieval
          │
          ├── for each chunk (concurrent dense+sparse):
          │     ├── Embedder.Embed()           ← Ollama nomic-embed-text (768-dim)
          │     └── SparseEmbedder.EmbedSparse() ← SPLADE sidecar (optional)
          │         exponential backoff retry on both
          │
          └── Store.Upsert()                  ← Qdrant gRPC upsert
                payload: file_path, header, line_start, chunk_index,
                         text, window_text, source_sha
                vectors: dense (named ""), sparse (named "sparse", optional)
```

### Search Pipeline

```
POST /search  { "query": "...", "top_k": 5 }
      │
      ▼
SearchHandler.ServeHTTP
  │
  ├── [SemanticCache enabled?]
  │     Embedder.Embed(query) → Qdrant cosine search on pkb_cache collection
  │     score >= threshold → return cached []ScoredChunk immediately
  │     miss → continue pipeline, write result to cache async after retrieval
  │
  ├── [keyword-only request?]
  │     Store.KeywordSearch() → Qdrant full-text scroll filter
  │
  ├── [HyDE enabled?]
  │     OllamaHyDEGenerator.Generate() × numDocs (parallel goroutines)
  │     Embedder.Embed() each hypothetical doc
  │     averageVectors() + L2-normalize
  │     Store.HybridSearch(avgVec, query, topK)
  │     fallback to multiSearch on generation failure
  │
  └── [standard path: multiSearch]
        splitFragments(query)          ← split on ./?/;
        for each fragment:
          Embedder.Embed(fragment)
          Store.HybridSearch(vec, fragment, topK)
        deduplicate by FilePath+LineStart, keep best score
              │
              ▼
        HybridSearch (QdrantStore)
          ├── [SPLADE sidecar available]
          │     server-side QueryPoints:
          │       prefetch dense (topK×5) + sparse (topK×5)
          │       Qdrant RRF fusion on server
          │
          └── [client-side fallback]
                dense: SearchPoints
                sparse: Scroll + filter text match + TFSparseScorer.Score()
                client-side RRF (k=60): score = Σ 1/(60 + rank)
              │
              ▼
        [Reranker enabled?]
          HTTPReranker: POST topK×candidateMul chunks to sidecar
          cross-encoder/ms-marco-MiniLM-L-6-v2 scores all candidates
          return top-k by cross-encoder score
              │
              ▼
        JSON response: []{ file_path, header, line_start, score, text }
```

---

## Interfaces (extension points)

```go
type Chunker        interface { Chunk(text, filePath string) ([]DocumentChunk, error) }
type Embedder       interface { Embed(ctx, text) ([]float32, error); Dimensions() int }
type SparseEmbedder interface { EmbedSparse(ctx, text, mode) ([]uint32, []float32, error) }
type SparseScorer   interface { Score(ctx, query, text string) (float64, error) }
type Store          interface { Upsert / DeleteByFile / Search / HybridSearch / KeywordSearch / GetFileSHA / GetAllFileSHAs }
type Fetcher        interface { FetchFile(ctx, path, sha string) (string, error) }
type FileLister     interface { ListMarkdownFiles(ctx, sha string) ([]FileEntry, error) }
type HyDEGenerator  interface { Generate(ctx, query string) (string, error) }
type Reranker        interface { Rerank(ctx, query string, chunks []ScoredChunk) ([]ScoredChunk, error) }
```

Swap any component by implementing its interface. Config selects providers; `httpserver/server.go` wires them.

---

## Dependencies

| Dependency | Purpose |
|-----------|---------|
| Go | Core runtime |
| [Goldmark](https://github.com/yuin/goldmark) | Markdown AST parsing for chunker |
| [Qdrant gRPC client](https://github.com/qdrant/go-client) | Vector DB: upsert, search, collections |
| [Ollama](https://ollama.com) (local) | Dense embeddings (`nomic-embed-text`, 768-dim) |
| Python sidecar `cmd/splade/main.py` | SPLADE sparse embeddings (`fastembed` + `splade-v3-distilbert`) |
| Python sidecar `cmd/reranker/main.py` | Cross-encoder reranking (`sentence-transformers`) |
| [gosdk/logger](https://github.com/Chandra179/gosdk) | Structured logging |
| gopkg.in/yaml.v3 | Config file parsing |

**External services required at runtime:**
- Qdrant (Docker: `docker compose up`)
- Ollama with `nomic-embed-text` pulled
- (optional) SPLADE sidecar: `python cmd/splade/main.py`
- (optional) Reranker sidecar: `python cmd/reranker/main.py`

---

## Feature
- [x] **Metadata filtering** — filter retrieval by `file_path`, `header`, or `source_sha` at query time. Benchmarks show +4.4 F1 and +2% context precision on domain-specific corpora ([arxiv 2510.24402](https://arxiv.org/html/2510.24402v1), [arxiv 2601.11863](https://arxiv.org/html/2601.11863v1)). Qdrant payload index used; all non-empty filter fields ANDed. Pass `"filter": {"file_path": "...", "header": "...", "source_sha": "..."}` in `POST /search` body.
- [x] **RAG answer generation** — `OllamaGenerator` streams LLM answer grounded in retrieved chunks. Enabled via `generator.enabled: true` in config. Triggered per-request with `"generate": true` in `POST /search` body. Uses "Lost in the Middle" chunk ordering (highest-scored at top+bottom, lowest in middle) and token budget truncation. Streams raw text tokens back to caller.
- [x] **Eval framework** — `RunEval()` runs queries concurrently (8 workers), scores results with MRR, Hit Rate (Success@K), NDCG, Precision, Recall@K, and MAP. `LLMRelevanceJudge` uses Ollama to label chunk relevance. Judgments cached as `Qrel` records to avoid re-judging. `qrelsJudge` implements `RelevanceCounter` so Recall@K and MAP are computed from total known-relevant counts. Enable via eval test harness in `internal/pkb/eval_harness_test.go`.
- [x] **PDF preprocessing sidecar** (`services/marker/`) — converts PDFs to Markdown using Marker. Produces accurate LaTeX math (`$...$`, `$$...$$`) where Docling emitted `<!-- formula-not-decoded -->`. Run one-shot (`--input pdfs/raw --output pdfs/converted`) or as FastAPI server on port 5003 (`POST /convert`, `GET /health`). Converted `.md` files feed the standard ingest pipeline.
- [x] **RAGAS-style context relevance scoring** ([arxiv 2309.15217](https://arxiv.org/abs/2309.15217)) — LLM scores each retrieved chunk for relevance to query (0–1 per chunk, averaged as `ContextRelevance`). Complements cross-encoder reranking (reranker reorders; this measures absolute relevance quality). `ContextScorer` interface in `eval_judge.go`; `LLMJudge.ScoreContext()` in `eval_judge_llm.go` (LOW→0.25/MEDIUM→0.5/HIGH→1.0); `EvalMetrics.ContextRelevance` aggregated in `eval_runner.go`. Opt-in via type assertion — no `RunEval` signature change.
- [ ] **Adaptive HyDE** ([arxiv 2507.16754](https://arxiv.org/abs/2507.16754)) — gate HyDE on retrieval confidence: run vanilla search first; apply HyDE only when top-1 cosine score < threshold. ~+20% helpfulness on developer QA; avoids retrieval pollution on high-confidence factoid queries. **Accuracy: high. Latency: neutral-to-better** (skips LLM call when not needed; adds one Qdrant round-trip when fired). Implement in `hyde.go`.
- [ ] **Multi-HyDE** ([arxiv 2509.16369](https://arxiv.org/abs/2509.16369)) — generate 3–5 diverse hypothetical docs per query (parallel goroutines), average embeddings, single Qdrant query. +11.2% accuracy, -15% hallucination rate vs. single-doc HyDE at same token cost. **Accuracy: high. Latency: +N×LLM_gen in parallel** (dominated by slowest generation; embedding overhead negligible). Implement in `hyde.go`.
- [ ] **Reranker candidate pool tuning** ([arxiv 2409.07691](https://arxiv.org/abs/2409.07691)) — increase `candidate_mul` so reranker sees 50–100 candidates. MRR@3 0.433→0.605 (+39.7%), Recall@5 +17.4%. **Accuracy: high. Latency: +linear with pool size** (cross-encoder is O(n); 100 candidates ≈ 2–5× slower rerank vs. default). Config-only change in `config.yaml`.
- [ ] **ChunkRAG post-retrieval filter** ([arxiv 2410.19572](https://arxiv.org/abs/2410.19572)) — LLM scores each retrieved chunk for relevance; drops irrelevant before generation. +10pp PopQA accuracy. Complements cross-encoder (drops vs. reorders). **Accuracy: medium-high. Latency: +1 LLM call per search** (can batch chunks in one prompt). New `pkb/chunk_filter.go`.
- [ ] **RAPTOR hierarchical indexing** ([arxiv 2401.18059](https://arxiv.org/abs/2401.18059), ICLR 2024) — recursive cluster→summarize tree over chunks at ingest; retrieve across all levels. +20% absolute accuracy on QuALITY (multi-hop QA). **Accuracy: high for multi-hop. Latency: ingest cost only** (search unchanged; tree stored in Qdrant alongside leaf chunks). Preprocessing layer over `RecursiveChunker` output.
- [ ] **MemoRAG global memory** ([arxiv 2409.05591](https://arxiv.org/abs/2409.05591), WWW 2025) — small LLM encodes full corpus into global KV cache; generates answer clues that guide retrieval for diffuse/global queries. Gains on LongBench/InfiniteBench where standard RAG fails. **Accuracy: high for corpus-wide reasoning. Latency: +1 small-LLM forward pass** (KV cache amortized; per-query clue generation ~100–300ms). Additive with HyDE pipeline.
- [ ] **Mix-of-Granularity chunking** ([arxiv 2406.00456](https://arxiv.org/abs/2406.00456), COLING 2025) — router model selects optimal chunk granularity per query (fine/coarse/multi-level). **Accuracy: medium. Latency: +router inference** (lightweight classifier; <10ms if local). Query-routing layer over `RecursiveChunker` multi-size outputs.
- [ ] **LongRAG large retrieval units** ([arxiv 2406.15319](https://arxiv.org/abs/2406.15319)) — retrieve fewer but larger document groups; let long-context LLM read them. HotpotQA +17.25% EM. **Accuracy: high for long-answer tasks. Latency: +LLM context cost** (reading large units costs more tokens; unsuitable for low-latency path). Re-index tradeoff; conflicts with small-chunk approach.
- [ ] **Contextual Retrieval chunking** ([Anthropic blog 2024](https://www.anthropic.com/news/contextual-retrieval), [AWS Bedrock](https://aws.amazon.com/blogs/machine-learning/contextual-retrieval-in-anthropic-using-amazon-bedrock-knowledge-bases/)) — LLM prepends a short context blurb per chunk before embedding, explaining where the chunk fits in the document. 49% fewer retrieval failures standalone; 67% with reranking. **Accuracy: very high. Latency: +1 LLM call per chunk at ingest-time only** (embedding + storage unchanged; production-deployed via Bedrock Knowledge Bases). Preprocessing step in `Pipeline.IngestFile()` before `Embedder.Embed()`.
- [ ] **LLM query rewriting** ([Azure AI Foundry blog 2024](https://techcommunity.microsoft.com/blog/azure-ai-foundry-blog/raising-the-bar-for-rag-excellence-query-rewriting-and-new-semantic-ranker/4302729), [Elastic search-labs](https://www.elastic.co/search-labs/blog/query-rewriting-llm-search-improve)) — rewrite user query before embedding: expand abbreviations, add date/entity context, paraphrase for retrieval. +30–45% precision in production (Azure AI Search ships this natively). **Accuracy: high. Latency: +1 small LLM call (~50–150ms)** (cacheable; same Ollama used by HyDE). New `pkb/query_rewriter.go`; plugs in before `Embedder.Embed()` in `SearchHandler`.
- [ ] **RAG-Fusion multi-query** ([LangChain/LlamaIndex engineering, community benchmarks](https://python.langchain.com/docs/how_to/MultiQueryRetriever/)) — generate N query reformulations in parallel, retrieve for each, fuse results via RRF. +20–30% recall vs. single query. **Accuracy: high. Latency: +N×embed+search in parallel** (dominated by LLM rewrite; N=3–5 typical). Complements HyDE (different reformulation strategy). New `pkb/query_fusion.go`.
- [ ] **Late chunking** ([arxiv 2409.04701](https://arxiv.org/abs/2409.04701), JinaAI 2024) — embed full document first, then pool token embeddings per chunk boundary instead of embedding chunks independently. Preserves cross-chunk context for anaphoric/pronoun-heavy documents (+10–12% on such corpora). **Accuracy: medium for pronoun-heavy docs. Latency: ingest-time only** (one full-doc embedding per file vs. N chunk embeddings; may be faster). Requires embedding model that returns token-level vectors (not Ollama `nomic-embed-text` today; needs model swap or JinaAI API).
- [ ] **RAG faithfulness monitoring / EvalOps** ([RAGAS monitoring docs](https://docs.ragas.io/en/v0.1.21/getstarted/monitoring.html), [DeepEval CI gates](https://docs.confident-ai.com/docs/getting-started), [Langfuse](https://langfuse.com/)) — sample X% of live queries, LLM-grade faithfulness + context relevance, alert on metric drift. Closes the loop between offline eval (existing `RunEval`) and production quality. **Accuracy: N/A (observability). Latency: async; no hot-path impact.** Instrument `SearchHandler` to emit traces; integrate RAGAS or DeepEval as CI quality gate on `internal/pkb/eval_*`.