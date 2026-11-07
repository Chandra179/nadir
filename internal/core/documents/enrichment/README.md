# Enrichment contract

Defines the optional contextual-retrieval capability used during indexing.
The contextual prompt, output cleanup and `Completer` seam live here; provider
protocol code belongs in `providers/ollama/chat` and `providers/cloudflare/chat`. Feature
flags and best-effort behavior belong to `indexing`.

Change here when the enrichment capability contract changes. Verify indexing
and the enrichment tests.
