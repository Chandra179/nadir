# Qdrant document Adapter

Persists indexed content and serves dense, BM25-style, and hybrid searches.
It owns Qdrant payloads, collection schema, aliases, point IDs, and protocol
errors. Indexing and Retrieval own document-version and ranking policy.

Change here for Qdrant schema, payload, search, or reset mechanics. Verify
with the package tests and Qdrant integration tests for schema changes.
