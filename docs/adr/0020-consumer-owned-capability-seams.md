# 0020 — Keep provider contracts public and consumer capability seams narrow

- **Status:** Accepted
- **Date:** 2026-09-12
- **Deciders:** Chandra, Codex

## Context

The Document Store and chat-history Adapter interfaces combined query,
indexing, read, mutation, reset, and diagnostic operations. Individual
consumers used only subsets of those methods, so test doubles implemented
unrelated behavior and changes to one capability propagated across unrelated
Modules. The Chat Module also repeated the Generator contract, and cache
collection provisioning was exposed as a runtime cache operation.

## Decision

- Keep one provider-owned exported contract for the main capability of a
  Module where that contract is useful to sibling consumers.
- Do not publish a broad aggregate contract for an infrastructure Adapter when
  all real callers use narrower consumer seams.
- Define narrow, package-private consumer seams for each distinct capability:
  Retrieval searches, Document indexing, Document reset, history reads, and
  Chat lifecycle persistence.
- Use `search.Retriever` as the shared Retrieval use-case contract instead of
  repeating identical consumer Interfaces in Chat and evaluation.
- Use `generator.Generator` directly in Chat; Chat does not re-export a
  structurally identical Generator contract.
- Keep semantic-cache runtime operations separate from startup collection
  provisioning.
- Retain capability Interfaces such as batch embedding, document intake, and
  lifecycle coordination when they represent optional behavior or a real
  change seam.

## Consequences

Consumers now receive only the leverage they need, and their tests can use
small Adapters without implementing unrelated methods. The Store and History
Modules remain intact because their operations share Document and Session
invariants; only their consumer seams are narrowed. Provider-owned contracts
remain available for real sibling consumers, while package-private seams keep
implementation ports from becoming accidental long-term APIs.
