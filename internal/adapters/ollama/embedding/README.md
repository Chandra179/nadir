# Ollama embedding Adapter

Implements the neutral `internal/embedding` contract with Ollama's embedding
API, including batch requests and readiness inspection. Prefix policy and
index/query usage belong to indexing and Retrieval, not this Adapter.

Change here for Ollama request/response or model probing. Configuration is
owned by `platform/configuration/`; callers are wired in `platform/server/`.

Verify with `go test ./internal/adapters/ollama/embedding`.
