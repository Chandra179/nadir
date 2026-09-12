# Internal packages

Private Go code is organized by bounded context. Start with the README in the
folder that owns the behaviour, then follow its related-folder table. The
folders are deliberately small enough that a task should have one clear owner:

| Task | Start here | Usually related |
|------|------------|-----------------|
| Normalize, chunk, enrich, embed, or publish Documents | `knowledge/` | `adapters/`, `retrieval/cache/` |
| Rewrite or rank a query and select context | `retrieval/` | `adapters/`, `conversation/chat/` |
| Sessions, turns, edit/prune, generation, or replay | `conversation/` | `retrieval/`, `adapters/qdrant/history/` |
| HTTP JSON, SSE, routes, or status mapping | `transport/http/` | owning bounded context, `contracts/` |
| Qdrant, Ollama, Docling, or reranker protocol | `adapters/` | the consumer seam and `platform/configuration/` |
| Startup wiring, configuration, middleware, logs, or lifecycle | `platform/` | `cmd/`, affected package |
| Retrieval quality metrics and golden queries | `evaluation/` | `retrieval/`, `test/evaluation/` |

## Dependency map

Arrows mean “calls or depends on.” `platform/lifecycle` is the composition
root: it constructs concrete Adapters and injects them into the bounded
contexts and HTTP transport. It is not a runtime business-flow owner.

### Runtime call flow

```text
Client
  │
  ▼
transport/http
  ├── POST documents ──────▶ knowledge/indexing
  │                           ├──▶ knowledge/chunking
  │                           ├──▶ knowledge/enrichment ──▶ adapters/ollama/enrichment
  │                           ├──▶ adapters/ollama/embedding
  │                           ├──▶ adapters/qdrant/documents
  │                           └──▶ retrieval/cache ──▶ adapters/qdrant/shared
  │
  ├── POST documents/reset ─▶ adapters/qdrant/documents
  │
  ├── POST turns ──────────▶ conversation/chat
  │                           ├──▶ retrieval/rewriting ──▶ adapters/ollama/rewriter
  │                           ├──▶ retrieval/search
  │                           │     ├──▶ retrieval/cache
  │                           │     ├──▶ adapters/ollama/embedding
  │                           │     ├──▶ adapters/qdrant/documents
  │                           │     └──▶ adapters/reranker
  │                           ├──▶ conversation/generation
  │                           │     └──▶ adapters/ollama/generator
  │                           └──▶ adapters/qdrant/history
  │
  └── sessions/history ────▶ adapters/qdrant/history

evaluation ─────────────▶ retrieval/search   (cache bypassed for measurement)
```

### Package dependency direction

```text
cmd/api
  │
  ▼
platform/lifecycle  ── constructs and injects
  ├──▶ transport/http
  ├──▶ knowledge/*
  ├──▶ retrieval/*
  ├──▶ conversation/*
  └──▶ adapters/*

transport/http ──▶ bounded contexts ──▶ consumer-owned Interfaces
       │                    ▲                       ▲
       └──▶ HTTP contracts  │                       │
                            └── Adapters implement ─┘

adapters/qdrant/{documents,history}
  └──▶ adapters/qdrant/shared       (shared infrastructure only)
```

The first diagram is runtime behaviour. The second is the intended ownership
direction for package dependencies: a bounded context defines the meaning of a
capability, an Adapter implements that capability, and lifecycle supplies the
concrete implementation. An Adapter may import a consumer-owned Interface to
implement it; that import is not a call back into the consumer.

### How to detect a circular dependency

The safe direction is:

```text
transport  ──▶ bounded context ──▶ seam/Adapter
                    ▲                 │
                    └── wired by ─────┘
                         platform/lifecycle
```

Never add an arrow from a bounded context or Adapter to `transport/http/` or
`platform/lifecycle/`. Never make an Adapter call a handler. If a lower-level
package needs behaviour from a higher-level package, define the narrow seam at
the caller and inject an implementation from lifecycle. If two bounded
contexts need each other directly, stop and reconsider ownership before adding
an import; usually one context should expose a smaller Interface or the shared
value should move to a neutral contract package.

## Task placement rules

- Change an existing folder when the concept already belongs to it.
- Add a new folder only for a new concept with its own lifecycle, data, or
  meaningful seam. Do not create a folder for one helper or one endpoint.
- Put provider protocol details in an Adapter; keep business rules in the
  consuming bounded context.
- Put HTTP translation in `transport/http`; do not make domain packages know
  about Gin, JSON, SSE, or status codes.
- Keep composition in `platform/lifecycle`; domain packages must not import
  transport or lifecycle packages.
- Expose a small provider-owned Interface only when another package consumes
  it. Keep implementation ports private and add compile-time assertions.

Each child README documents the local contract, related work, and verification.
The root architecture and ADRs remain authoritative for cross-folder design.
