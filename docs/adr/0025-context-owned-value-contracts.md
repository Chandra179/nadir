# 0025 — Keep value contracts with their owning context

- **Status:** Accepted
- **Date:** 2026-09-13
- **Deciders:** Chandra, Codex architecture review

## Context

The temporary `internal/content` package became a shared change boundary for
indexing, retrieval, caching, and persistence. Although it had no provider
dependencies, it still made unrelated teams coordinate through one grab-bag
package and obscured which context owned each field.

## Decision

- Knowledge indexing owns `IndexedChunk` and the document-publication values.
- Retrieval search owns `SearchCandidate`, `Filter`, and caller-facing search
  results.
- Retrieval cache owns its cached candidate representation and cache entries.
- Qdrant and other Adapters translate between the consumer-owned values and
  persistence records.
- A Module exposes at most one primary public behavioural interface. Interfaces
  needed only by that Module remain private in `private_interfaces.go`.

## Consequences

Each field now has a clear owner and a focused change boundary. Adapters have a
small amount of explicit mapping code, but domain packages no longer depend on
a shared neutral package or on one another's internal value model. A contract
that truly becomes a stable cross-context protocol must be promoted explicitly
through a new ADR rather than added to a general-purpose package.
