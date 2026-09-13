# Inference resources

This Module provides process-local admission control for expensive model
operations. The shared Ollama Gate covers embedding, rewriting, enrichment,
and streaming generation; the reranker has its own Gate because it is a
separate process and may run on a different device.

The configured local profile is conservative: one Ollama operation and one
reranker operation at a time, a bounded queue wait, and a finite Ollama
`keep_alive` value. It prevents one process from creating unbounded model
contention; it is not a distributed rate limiter. Multiple API instances need
the distributed resource controls described in `docs/SCALING.md`.
