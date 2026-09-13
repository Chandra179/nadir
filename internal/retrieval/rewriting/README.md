# Query-rewriting contract

Defines the capability for turning conversational follow-ups into standalone
search queries. Chat owns when rewriting is used and fallback behavior;
Ollama protocol code belongs in `adapters/ollama/rewriter`.

Change here when the rewriting capability contract changes. Verify Chat and
rewriter Adapter tests.
