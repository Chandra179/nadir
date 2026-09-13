# Configuration

Loads YAML, applies environment overrides, centralizes production defaults,
and validates explicit role-specific endpoints and models. Runtime packages
consume the validated configuration; they do not resolve cross-role fallbacks.

The `inference` section is the local resource profile: it bounds the shared
Ollama roles, gives queued model work a finite wait, and sends an explicit
`keep_alive` value on every Ollama request. The nested reranker resource
settings choose an explicit device/backend and cap sidecar concurrency.

Change here for a config key, environment mapping, default, or validation
rule. Verify with `go test ./internal/platform/configuration`.
