# 0024 — Keep shared content values outside bounded contexts

- **Status:** Superseded by [0025](0025-context-owned-value-contracts.md)
- **Date:** 2026-09-13
- **Deciders:** Chandra, Codex architecture review

## Context

Indexing, Retrieval, the semantic cache, and provider Adapters initially
exchanged values through a neutral `internal/content` package. That reduced
imports temporarily, but it made unrelated contexts share one change boundary.

## Decision

- This decision is retained as historical context only. See ADR 0025 for the
  current ownership model.

## Consequences

The neutral package was useful during the first extraction, but it was too broad
for the target bounded-context architecture.
