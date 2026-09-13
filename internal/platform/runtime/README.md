# Runtime

The Runtime Module is the composition seam shared by executable entry points.
It creates one Qdrant connection, the document Adapter, embedding Adapter,
indexing pipeline, optional semantic cache, optional reranker, and retrieval
service. It exposes capabilities and lifecycle functions rather than storage
implementations.

Use this Module when an executable needs the shared retrieval/indexing graph.
The API server adds HTTP, history, generation, and readiness wiring around it;
the evaluator uses the same graph without starting an HTTP listener.

The runtime is process-local. Qdrant provides shared persistence, but indexing
and reset coordination remain single-node until a distributed lease/fencing
mechanism is introduced.
