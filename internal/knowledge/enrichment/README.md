# Enrichment contract

Defines the optional HyPE and contextual-retrieval capabilities used during
indexing. Provider implementations belong in `adapters/ollama/enrichment`;
feature flags and best-effort behavior belong in `knowledge/indexing`.

Change here when the enrichment capability contract changes. Verify indexing
and Ollama enrichment tests.
