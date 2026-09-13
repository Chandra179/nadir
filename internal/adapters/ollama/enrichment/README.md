# Ollama enrichment Adapter

Implements HyPE question generation and contextual-intro generation for the
Knowledge indexing context. It owns Ollama protocol details, output cleanup,
timeouts, and provider errors; feature flags and fallback policy belong to
`knowledge/indexing`.

Change here for the Ollama enrichment contract. Verify with
`go test ./internal/adapters/ollama/enrichment`.
