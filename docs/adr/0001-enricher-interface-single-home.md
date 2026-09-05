# 0001 — The Enricher interface lives in `internal/enrichment`, next to its implementation

- **Status:** Accepted
- **Date:** 2026-09-05
- **Deciders:** Chandra, ZCode session

## Context

The `Enricher` interface (HyPE hypothetical questions + Anthropic-style
contextual intros) was defined twice: once in `internal/enrichment` (next to
the Ollama implementation) and once in `internal/ingest` as a consumer-side
duplicate. Go's structural typing made both compile, but the duplication had
costs: two places to keep method docs in sync, and ambiguity about which
definition is authoritative — `cmd/evalbench` and `internal/server` wired the
enrichment implementation through the ingest-side copy.

The repo also split every package's interfaces into per-package
`interface.go` files (commit `7a6c178`), which made the duplication visible.

## Decision

`internal/enrichment/interface.go` is the **single** definition of
`Enricher`, co-located with its implementation. `internal/ingest` imports
`nadir/internal/enrichment` and consumes `enrichment.Enricher` directly
(`ingest.DependenciesConfig`, `WithEnrichment`). The duplicate interface in
`internal/ingest/interface.go` was deleted with **no backward-compat shim** —
no alias, no re-export. The method-level doc comments moved with the
interface.

## Consequences

- One place to document and change the enrichment contract; the compiler
  flags every consumer on any signature change.
- `internal/ingest` now depends on `internal/enrichment`. Both are domain
  packages, so the "no api/server/middleware imports" rule still holds.
- Convention going forward: when a package owns an abstraction (like
  `enrichment`), its interface lives there; consumer-side narrow interfaces
  (like `chat.Searcher`) remain for slices of packages owned elsewhere.
