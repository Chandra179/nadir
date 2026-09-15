# Admission

This Module owns process-wide admission and backpressure for expensive or
destructive work. It provides one bounded queue per operation: Retrieval
fragments, reranking, generation, embedding, indexing, and destructive
mutations.

The interface is a shared `Gate` capability: callers wait for a finite time,
receive a capacity error when the budget is full, and release the slot after
the complete operation finishes. Streaming generation holds its slot until
the stream closes.

The controller is local to one executable. It prevents concurrent HTTP
requests in that process from multiplying per-request limits, but it does not
coordinate multiple API instances. A distributed deployment needs a shared
queue, lease, or rate-limit Adapter behind this seam.
