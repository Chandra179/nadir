# Qdrant Adapters

Qdrant persistence is split by owning data lifecycle. The children translate
Qdrant gRPC and payloads, while the calling bounded context decides what a
Document, Session, cache entry, or reset means.

| Data or concern | Folder | Owner of policy |
|---|---|---|
| Indexed Document chunks and reset generations | `documents/` | `knowledge/` and `retrieval/` |
| Sessions and persisted Chat turns | `history/` | `conversation/` |
| Shared clients, collections, and value codecs | `shared/` | Adapter infrastructure only |

Change here for Qdrant API calls, payload encoding, indexes, retry-safe
replacement, aliases, and persistence errors. Change the bounded context for
ordering, mutation ownership, cache invalidation, or domain invariants. Do not
put one global repository abstraction around all Qdrant data.

## Verification

Run unit tests for the affected child. Run Qdrant integration tests and reset
generation checks when schema, alias, payload, or collection lifecycle changes.
