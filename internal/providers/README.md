# Adapters

Adapters translate external protocols into the seams consumed by Nadir's
bounded contexts. They contain HTTP/gRPC clients, serialization, timeouts, and
provider-specific error handling; they do not decide indexing, retrieval,
session, or generation policy.

## Task routing

| If the task is about... | Start here | Also inspect |
|---|---|---|
| PDF-to-Markdown intake | `docling/` | `core/documents/indexing/`, configuration |
| Ollama embeddings or LLM calls | `ollama/` | the consuming seam and config |
| Semantic cache in Vectorize and KV | `cloudflare/cache/` (adapter), `cloudflare/vectorize/`, `cloudflare/kv/` | `core/retrieval/cache/`, config, ADR 0042 |
| Chat history in D1 | `cloudflare/history/` (rows) and `cloudflare/d1/` (REST client) | `core/conversation/chat/`, config, ADR 0041 |
| Workers AI embeddings | `cloudflare/embedding/` | `core/embedding/`, config, ADR 0040 |
| Workers AI answer generation | `cloudflare/generator/` | `core/conversation/chat/`, config |
| Workers AI rewrite and enrichment completions | `cloudflare/chat/` | rewriter and enrichment seams, config |
| Cloudflare REST transport (auth, errors, retry) | `cloudflare/cfapi/` | `cloudflare/workersai/`, `d1/`, `vectorize/`, `kv/` |
| Workers AI HTTP client (`/ai/v1`) | `cloudflare/workersai/` | every Workers AI adapter |
| Ollama rewrite and enrichment completions | `ollama/chat/` | rewriter and enrichment seams, config |
| Qdrant persistence or schema | `qdrant/` | the owning context and reset rules |
| Cross-encoder reranking | `reranker/` | `core/retrieval/search/` |
| A distributed event backend | a new Adapter after design approval | `core/conversation/chat/`, `docs/architecture.md` |

Change an Adapter when an external request/response contract or client
behaviour changes. Change the bounded context when the meaning, ordering,
fallback policy, or business invariant changes. Add a new Adapter directory for
a genuinely different provider, not for another function in an existing one.

## Rules

Adapters may depend on domain interfaces and value types, but domains must not
depend on Adapter implementations. Wire concrete Adapters only from
`bootstrap/server`. Keep network calls cancellable and bounded by role
configuration. Test protocol decoding, cancellation, non-success responses,
and malformed provider responses at the Adapter seam.
