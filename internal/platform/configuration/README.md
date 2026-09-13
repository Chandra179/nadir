# Configuration

Loads YAML, applies environment overrides, centralizes production defaults,
and validates explicit role-specific endpoints and models. Runtime packages
consume the validated configuration; they do not resolve cross-role fallbacks.

Change here for a config key, environment mapping, default, or validation
rule. Verify with `go test ./internal/platform/configuration`.
