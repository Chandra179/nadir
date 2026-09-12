# Nadir system architecture

Nadir is a modular monolith with a separately deployable web client.

The system has four architectural layers: a browser dashboard, a JSON/SSE
API, domain Modules, and infrastructure Adapters. The browser features own
their client-side contracts and state; the API maps those contracts to domain
use-cases; domain Modules do not depend on the transport; and one composition
boundary wires concrete Adapters. A separate evaluation application reuses
the Retrieval and indexing Modules without becoming part of the serving path.

```text
React + TypeScript dashboard
          │ JSON commands/queries + SSE answer stream
          ▼
      Go HTTP API
          │
          ├── Chat lifecycle: sessions, edits, generation supervision
          ├── Retrieval: query rewrite → hybrid search → RRF → rerank → cache
          ├── Document intake: discover/upload → chunk → enrich → embed → replace
          └── Evaluation: golden-set quality measurements
          │
          ├── Qdrant: documents, semantic cache, chat history
          ├── Ollama: embeddings and optional LLM roles
          ├── Reranker sidecar
          └── Optional Docling sidecar for PDF conversion
```

The browser has no server-rendered HTML dependency. It consumes a versioned
JSON API and one SSE stream per generated turn. The API owns ordering,
mutation, cancellation, persistence, and bounded event retention.

Operationally, `/api/v1/health` is dependency-free liveness, while
`/api/v1/ready` verifies Qdrant, the configured embedding model, and the
enabled reranker before an instance should receive Retrieval traffic.

Retrieval combines dense semantic search and BM25 keyword search using
Reciprocal Rank Fusion. An optional cross-encoder reranks the leading
candidates. The answer generator receives the selected source passages and
streams plain text back to the client.

Chat sessions are ordered timelines. Editing a turn is intentionally
destructive: the selected turn and every later turn are pruned, then the
replacement is appended to the same session. Document replacement is
versioned and published only after the new version is ready; reset publishes a
new empty collection before retiring the old one.

The default deployment is single-node. Qdrant and model sidecars are external
processes, but Chat streams and mutation coordination remain process-local.
The React dashboard runs locally through Vite; Docker Compose supplies the
backend services only. A production deployment may serve the built dashboard
from an independently managed static host.
See [SCALING.md](SCALING.md) for the distributed-system boundary and required
shared ordering/fencing work.
