# Ollama Adapters

This folder groups outbound Ollama Adapters by role. Each child translates one
provider protocol while the consuming bounded context owns when and why the
call is made.

| Role | Folder | Consumer |
|---|---|---|
| Dense embeddings | `embedding/` | `knowledge/indexing/`, `retrieval/search/`, `retrieval/cache/` |
| HyPE and contextual enrichment | `enrichment/` | `knowledge/indexing/` |
| Streaming answer generation | `generator/` | `conversation/chat/` |
| Conversational query rewriting | `rewriter/` | `conversation/chat/` through `retrieval/rewriting/` |

Change a child for Ollama request/response, streaming, timeout, or model
protocol changes. Change the consumer for prompt policy, call ordering,
optional-feature behaviour, or persistence. Every role has explicit endpoint
and model configuration; do not add fallback resolution between roles.

## Verification

Run the affected child package tests and update its HTTP contract tests when a
provider payload changes. Keep calls cancellable and avoid embedding prefixes
inside this folder; prefixes are applied at the call sites that know the task.
