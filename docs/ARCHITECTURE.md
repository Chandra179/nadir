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

## Semantic Cache Design

Cache sits in a **separate Qdrant collection** (`pkb_cache` by default). No extra infra needed.

**Flow:**
1. Incoming query → `Embedder.Embed(query)` → cosine search in `pkb_cache`
2. Top-1 result score ≥ threshold (default 0.90) → deserialize `results_json` payload → return immediately
3. Miss → run full search pipeline → fire-and-forget goroutine writes `{query_vec, results_json, cached_at}` to cache
4. TTL check: if `cached_at` + TTL < now → treat as miss (lazy expiry)

**Enable:** set `semantic_cache.enabled: true` in `config/config.yaml`.