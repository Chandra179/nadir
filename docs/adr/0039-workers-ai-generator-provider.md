# 0039 — Workers AI as an optional answer-generation provider

- **Status:** Accepted
- **Date:** 2026-11-07
- **Deciders:** Chandra

## Context

Answer generation ran only on a local Ollama model, which needs a GPU or a slow
CPU. Cloudflare Workers AI serves hosted chat models through an
OpenAI-compatible endpoint (`/accounts/{id}/ai/v1/chat/completions`). The
`Generator` seam (ADR 0020) is a transport, so a second provider needs no
change to Conversation.

## Decision

- `generator.provider` selects `ollama` (default) or `cloudflare`. The
  Cloudflare adapter lives in `internal/providers/cloudflare/generator` and
  decodes SSE into the same Token / Error / Done events.
- Credentials come from `CLOUDFLARE_ACCOUNT_ID` and `CLOUDFLARE_API_TOKEN`. The
  token is env-only: YAML containing it is rejected, and it is never logged or
  included in errors.
- `think`, `num_ctx` and `keep_alive` do not apply. `generator.num_ctx` remains
  the prompt budget for chat.
- Readiness reports configuration only. Probing a hosted API would spend quota
  and fail on transient network errors; an actual turn establishes reachability.
- Ollama stays the default. Making Workers AI the default requires a Ragas
  comparison on the reviewed dataset against the current answer model
  (ADR 0035, 0036), because the available models differ.
- `rewriter.provider` and `enrichment.contextual.provider` select `ollama`
  (default) or `cloudflare` independently, each with its own `model`. All
  Cloudflare roles share the top-level `cloudflare.account_id` and
  `CLOUDFLARE_API_TOKEN`.
- Rewrite and enrichment prompts live in `core/conversation/rewriting` and
  `core/documents/enrichment`, which own a `Completer` seam. `ollama/chat` and
  `cloudflare/chat` are the two completer adapters; output caps are 128 tokens
  (rewrite) and 256 (contextual intro). Contextual enrichment changes chunk
  text, so switching its provider needs a reindex.
- `cloudflare/workersai` is the one HTTP client under every Cloudflare adapter:
  account-scoped URL, bearer auth, status-error decoding, and retry of 429/503
  honoring `Retry-After` (two retries), because per-chunk enrichment can exceed
  the text-generation rate limit.

## Consequences

- Prompts, retrieved document text and answers leave the machine for Cloudflare
  when the provider is `cloudflare`.
- A rewrite adds a network round trip before retrieval; its 8s timeout and
  fallback to the raw query still apply. Indexing sends every chunk and its
  document excerpt to Cloudflare when contextual enrichment uses `cloudflare`.
- Every answer is a network round trip and is billed in Workers AI neurons
  after the free allocation; text generation defaults to 300 requests per minute
  per account.
- Models that spend output tokens on reasoning can exhaust the 512-token budget
  and now fail with an explicit error instead of an empty answer.
- Not yet verified against the live API: the sandbox cannot reach Cloudflare, so
  the adapter is tested against a fake OpenAI-compatible SSE server only.
