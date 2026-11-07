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
  (default) or `cloudflare` independently. They share the generator's account id
  and `CLOUDFLARE_API_TOKEN`, use their own `model`, and call the non-streaming
  `internal/providers/cloudflare/chat` client through a `Completer` seam in the
  existing rewriter and enrichment packages, so prompts and output cleanup stay
  unchanged. Output caps are 128 tokens (rewrite) and 256 (contextual intro).
  Contextual enrichment changes chunk text, so switching its provider needs a
  reindex.

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
