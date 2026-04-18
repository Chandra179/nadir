# Skeleton: Evolutionary Architecture (Golang)

This repo is an example for golang template project, modules name, functionality is just an example

## Architectural Definitions
* **Component (The App):** This entire repository is a single Component. It is an independently deployable unit that provides a set of related business capabilities.
* **Modules (Internal Logic):** Located in `internal/modules/`, these are logical wrappers (Go packages) used to maintain high **Functional Cohesion**. 

## Why this Structure?
1.  **Modularity:** Logic is partitioned by domain (`order`, `calc`) rather than technical layers.
2.  **Fitness Functions:** This structure allows you to write tests (e.g., using `ArchGuard` or `go-cyclomatic`) to ensure the `calc` module doesn't accidentally start importing `httpserver` logic.
3.  **Evolutionary Path:** If the `calc` module's architecture characteristics change (e.g., it needs massive scalability), it is decoupled enough to be extracted into a separate **Architecture Quantum**.

# Personal Knowledge Base

#### Concept

An intelligent search and retrieval layer for Markdown-based knowledge bases. It transforms static notes into a queryable brain by combining traditional text processing with vector embeddings.

#### Goals

* **High Precision Retrieval:** Ensure users find the exact context, not just the file.
* **Architectural Modularity:** Decouple the chunking logic and retrieval strategies to allow for experimentation with different LLMs or Vector DBs.
* **Cost Efficiency & Privacy:** Minimize token usage via RAG and support local embedding models to keep personal data private.
* **Low Latency:** Provide sub-second search results using optimized vector indexing.

#### Engine Components

* **Chunker** (`RecursiveChunker`): splits by heading → paragraph → sentence → word with configurable size + overlap. Goldmark-parsed headings become source pointers.
* **Embedder** (`OllamaEmbedder`): local Ollama instance (e.g. `nomic-embed-text`). Swappable via `Embedder` interface.
* **Store** (`QdrantStore`): Qdrant via gRPC. Upsert, delete-by-file, cosine similarity search, SHA-based dedup.
* **Pipeline**: chunk → embed (exponential backoff retry) → upsert. SHA dedup skips unchanged files.
* **Ingestion source**: git submodule (`gitbook/`). `LocalFileLister` walks for `.md` files; `LocalFetcher` reads content.

#### API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/ingest` | Walk submodule, skip unchanged (SHA match), ingest new/modified files |
| `POST` | `/search` | Embed query → vector search → return ranked chunks with file+header+line pointers |

#### Interfaces (extension points)

```go
type Chunker  interface { Chunk(text, filePath string) ([]DocumentChunk, error) }
type Embedder interface { Embed(ctx context.Context, text string) ([]float32, error); Dimensions() int }
type Store    interface { Upsert/DeleteByFile/Search/EnsureCollection/GetFileSHA }
type Fetcher  interface { FetchFile(ctx context.Context, path, sha string) (string, error) }
```

#### Dependencies

* **Language:** Go
* **Markdown Parser:** Goldmark
* **Vector DB:** Qdrant (local Docker or Cloud)
* **Embeddings:** Ollama (`nomic-embed-text`) — local, private