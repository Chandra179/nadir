# 0040 — Workers AI as an optional embedding provider

- **Status:** Accepted
- **Date:** 2026-11-07
- **Deciders:** Chandra

## Context

Embeddings ran only on a local Ollama model. Workers AI serves embedding models
through an OpenAI-compatible `/ai/v1/embeddings` endpoint. The embedding
capability is already a provider-neutral seam (`core/embedding`), and the
shared Workers AI client (ADR 0039) supplies auth, errors and 429 retry.

## Decision

- `embedder.provider` selects `ollama` (default) or `cloudflare`. The adapter is
  `internal/providers/cloudflare/embedding`; credentials come from the shared
  `cloudflare` section and `CLOUDFLARE_API_TOKEN`.
- `EmbedBatch` is implemented and splits into requests of 32 inputs. Results
  are placed by the response `index`, not arrival order. Every returned vector
  must have `embedder.dimensions` entries; a mismatch fails the request with a
  message naming the config key.
- Readiness reports the configured model and vector size without a network
  call, for the reasons in ADR 0039. The per-response dimension check catches a
  wrong `embedder.dimensions` on the first embed.
- `num_gpu` and `keep_alive` do not apply. Query and document prefixes are
  unchanged and still configured under `embedder`.
- Ollama stays the default. A Ragas comparison (ADR 0036) must show no
  retrieval regression before Workers AI becomes the default.

## Consequences

- Changing provider or model changes the vector space even when the dimension
  is the same, so existing vectors are not comparable with new query vectors.
  There is no automatic guard: reset and reindex, or point `qdrant.collection`
  at a new collection. The semantic cache invalidates itself because its policy
  fingerprint includes `embedder`. Chat history vectors are also in the old
  space; use a new `history.collection` when keeping history.
- Document text, chunks and every search query leave the machine. Every query
  now pays a network round trip before retrieval.
- Workers AI embedding models have small input limits (512 tokens for
  embeddinggemma-300m, from provider documentation not re-checked here). Lower
  `embedder.max_input_chars` accordingly; the Ollama default of 8000 characters
  exceeds it.
- Not yet verified against the live API: the sandbox cannot reach Cloudflare, so
  the request and response shape, the batch limit and model availability are
  tested against a fake server only.
