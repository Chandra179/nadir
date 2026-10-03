# Ollama embedding Adapter

Implements the neutral `internal/embedding` contract with Ollama's embedding
API, including batch requests and readiness inspection. Prefix policy and
index/query usage belong to indexing and Retrieval, not this Adapter.

Change here for Ollama request/response or model probing. Configuration is
owned by `bootstrap/configuration/`; callers are wired in `platform/server/`.

Optional `embedder.num_gpu` (`EMBEDDER_NUM_GPU`) controls Ollama GPU layers:
`0` explicitly uses CPU, `-1` delegates to Ollama, and positive values request
partial/full layer offload. Omitting it preserves Ollama's normal placement.
This lets a small embedder coexist with a larger answer model on limited GPU
memory. It changes placement, not model identity, task prefixes or concurrency;
measure retrieval and interactive latency before changing a local profile.

Verify with `go test ./internal/providers/ollama/embedding`.
