# Adapters

Adapters translate external protocols into the seams consumed by Nadir's
bounded contexts. They contain HTTP/gRPC clients, serialization, timeouts, and
provider-specific error handling; they do not decide indexing, retrieval,
session, or generation policy.

## Task routing

| If the task is about... | Start here | Also inspect |
|---|---|---|
| PDF-to-Markdown intake | `docling/` | `knowledge/indexing/`, configuration |
| Ollama embeddings or LLM calls | `ollama/` | the consuming seam and config |
| Qdrant persistence or schema | `qdrant/` | the owning context and reset rules |
| Cross-encoder reranking | `reranker/` | `retrieval/search/` |
| A distributed event backend | a new Adapter after design approval | `conversation/chat/`, `docs/SCALING.md` |

Change an Adapter when an external request/response contract or client
behaviour changes. Change the bounded context when the meaning, ordering,
fallback policy, or business invariant changes. Add a new Adapter directory for
a genuinely different provider, not for another function in an existing one.

## Rules

Adapters may depend on domain interfaces and value types, but domains must not
depend on Adapter implementations. Wire concrete Adapters only from
`platform/server`. Keep network calls cancellable and bounded by role
configuration. Test protocol decoding, cancellation, non-success responses,
and malformed provider responses at the Adapter seam.
