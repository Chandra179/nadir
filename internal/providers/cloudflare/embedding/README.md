# Cloudflare Workers AI embedding Adapter

Implements `core/embedding.Embedder` (plus batch embedding) over Workers AI's
OpenAI-compatible endpoint (`POST /accounts/{id}/ai/v1/embeddings`). It owns the
wire format, batching into requests of 32 inputs, ordering by response `index`,
and the check that every vector has `embedder.dimensions` entries. Prefixes,
input clamping, reindexing and collection policy belong to the callers.

HTTP, bearer auth, error decoding and 429/503 retry come from
`cloudflare/workersai`; credentials come from the shared `cloudflare` config
section and `CLOUDFLARE_API_TOKEN`.

Switching provider or model changes the vector space: reset and reindex (see
ADR 0040). Readiness reports configuration only; there is no network probe.

Change here for provider protocol behavior. Verify with
`go test ./internal/providers/cloudflare/embedding`.
