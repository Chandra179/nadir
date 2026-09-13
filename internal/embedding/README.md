# Embedding contract

Defines the provider-neutral embedding capability used by indexing, Retrieval,
semantic caching, and history persistence. Ollama or another provider belongs
in `adapters/`; this package owns no HTTP protocol or model policy.

Adapters may implement an additional `EmbedBatch` method internally to reduce
provider round trips. Batch support is detected by the consuming indexing or
retrieval Module and is not part of the public embedding contract.

The public contract is the single `Embedder` interface in `interface.go`.
