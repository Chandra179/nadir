# Integration tests

Integration tests that need Qdrant, Ollama, the reranker, or Docling remain
co-located with the Go Adapter or domain package they exercise. Run them with
the repository's normal Go test command after starting the required services.

This directory is reserved for future cross-process scenarios that exercise
more than one bounded context or deployment service.
