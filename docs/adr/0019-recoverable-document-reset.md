# 0019 — Recoverable Document reset through collection generations

- **Status:** Accepted
- **Date:** 2026-09-12
- **Deciders:** Chandra, Codex

## Context

The Document Store needs a destructive reset when its vector schema changes or
the user requests a full clear. Deleting the configured collection before
recreating it creates a period where Retrieval is unavailable, and a failed
recreation can leave the corpus permanently missing. A point-by-point delete
does not solve schema replacement and is slower for a full reset.

## Decision

The Qdrant Document Adapter treats the configured collection name as the
legacy/bootstrap physical collection and uses a stable derived active alias for
all Document reads and writes.

1. On startup, an existing configured collection is validated and adopted by
   the active alias. A missing collection is created and then adopted.
2. Reset creates a uniquely named empty generation with the complete expected
   dense, sparse, and payload-index schema.
3. The active alias is switched to the new generation in one Qdrant alias
   operation. Failed creation or publication leaves the old generation active.
4. Old physical generations are deleted only after publication. Cleanup
   failures return an explicit retryable error while the new generation stays
   active; later reset cleanup can remove the retired collection.
5. The process-local Document lifecycle gate prevents a complete Indexing pass
   from racing a reset. The Store also serializes reset with individual
   replacements and searches. Distributed deployments still require a shared
   lease or fencing token.

## Consequences

Reset has a temporary two-generation storage cost and leaves old collections
behind when Qdrant cleanup is unavailable. That is preferable to making the
active corpus unavailable or deleting the newly published generation. The
active alias adds a small bootstrap migration and makes collection names an
implementation detail of the Store Adapter. Qdrant remains the persistence
authority; this decision does not make Chat history or Indexing distributed-safe.
