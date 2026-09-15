# Configuration

Loads YAML, applies environment overrides, centralizes production defaults,
and validates explicit role-specific endpoints and models. Runtime packages
consume the validated configuration; they do not resolve cross-role fallbacks.

The `inference` section is the local resource profile: it bounds the shared
Ollama roles, gives queued model work a finite wait, and sends an explicit
`keep_alive` value on every Ollama request. The nested reranker resource
settings choose an explicit device/backend and cap sidecar concurrency.

The `admission` section adds process-wide operation budgets with finite queue
timeouts for Retrieval fragments, reranking, generation, embedding, indexing,
and destructive mutations. History session and turn page sizes are also
explicit configuration rather than transport or Qdrant Adapter constants.

Profiling is disabled by default. When enabled, validation requires a
loopback-only `profiling.addr`; remote diagnostics should use a protected
local path such as an SSH tunnel.

Change here for a config key, environment mapping, default, or validation
rule. Verify with `go test ./internal/platform/configuration`.
