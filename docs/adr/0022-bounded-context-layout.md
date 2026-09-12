# 0022 — Organize Go code by applications and bounded contexts

## Status

Accepted

## Decision

Executable entrypoints live under `cmd/`: `api` owns the HTTP process
entrypoint and `evaluator` owns Retrieval-quality measurement. Private Go code
lives under `internal/`, grouped by bounded context and technical ownership:
`knowledge`, `retrieval`, `conversation`, `evaluation`, `transport/http`,
`adapters`, and `platform`. The previous package layout is removed without
compatibility packages or import aliases.

Each Module owns its types, dependency wiring, Interfaces, Implementations,
and tests. Contexts communicate through provider-owned Service Interfaces and
the lifecycle Module wires concrete Adapters. The browser remains a separate
React feature application under `web/dashboard`.

Deployment assets are grouped under `deploy/compose`, `deploy/kubernetes`, and
`deploy/helm`. Compose is the supported single-node deployment today; the
Kubernetes and Helm directories document prerequisites for a future distributed
deployment rather than providing speculative active-active manifests.

## Consequences

- Teams can change Chat, Document intake, Retrieval, or evaluation with a
  narrower ownership surface.
- Application entrypoints stay thin and do not become a second domain layer.
- Go import paths now communicate bounded-context and Adapter ownership
  directly.
- Moving a Module is a deliberate breaking change; this migration does not
  preserve old package paths.
- A future indexer or administration process can be added under `cmd/` when
  it has a real lifecycle and contract; empty placeholder binaries are not
  created in advance.
