# Query-rewriting contract

Defines the capability for turning conversational follow-ups into standalone
search queries. Chat owns when rewriting is used and fallback behavior;
the prompt, output cleanup and `Completer` seam live here, and provider
protocol code belongs in `providers/ollama/chat` and `providers/cloudflare/chat`.

Change here when the rewriting capability contract changes. Verify Chat and
rewriting tests.
