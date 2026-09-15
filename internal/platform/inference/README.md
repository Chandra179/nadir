# Inference resources

This Module provides the low-level process-local Gate used by the shared
admission controller. The controller's operation budgets cover Retrieval
fragments, reranking, generation, embedding, indexing, and destructive
mutations; the Ollama Gate still protects the combined local model resource.

The configured local profile is conservative: one Ollama operation and one
reranker operation at a time, a bounded queue wait, and a finite Ollama
`keep_alive` value. It prevents one process from creating unbounded model
contention; it is not a distributed rate limiter. Multiple API instances need
the distributed resource controls described in `docs/SCALING.md`.
