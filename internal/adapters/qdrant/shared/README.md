# Shared Qdrant infrastructure

Provides shared clients, collection helpers, dense-vector setup, and primitive
payload codecs for the Qdrant Adapters. It must not own Document, cache, or
Conversation lifecycle policy.

Change here only for reusable Qdrant infrastructure. Verify with
`go test ./internal/adapters/qdrant/shared` and the affected Adapter tests.
