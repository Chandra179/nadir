# 0021 — Replace server-rendered dashboard with React and TypeScript

> Superseded by [ADR-0023](0023-local-vite-dashboard.md) for dashboard
> packaging and deployment. The React/TypeScript API contract decision remains
> current.

## Status

Superseded by ADR-0023

## Decision

The dashboard is a React + TypeScript + Tailwind application under
`web/dashboard`. The Go service exposes only versioned JSON and SSE endpoints;
the old HTMX, Alpine, embedded-template, and HTML-fragment paths are removed
without a compatibility layer.

The production dashboard is built as static assets and served by Nginx. Nginx
proxies `/api/` and SSE requests to the Go API. Vite provides the same proxy
shape for local development.

The Go bounded contexts remain an in-process modular monolith for now. Their
contracts are explicit, but Chat mutations, event replay, indexing ownership,
and cache invalidation still rely on single-process coordination. Splitting
them into microservices before shared ordering, fencing, and durable event
ownership exist would add network failure modes without improving the current
deployment. The scale-out prerequisites are documented separately in
`docs/SCALING.md`.

## Consequences

- Frontend and backend contracts are explicit in `contracts/http/openapi.yaml`
  and `contracts/events/chat-stream.md`.
- Browser state owns rendering, stream subscription, edit-tail pruning, and
  navigation; domain state and persistence remain in Go.
- The API binary no longer embeds or parses UI templates.
- Browser-level tests and dependency-backed E2E tests are now a separate
  frontend/operations concern; Go HTTP contract tests remain fast and local.
- Static hosting and the API can scale independently, while the current Chat
  event broker remains single-node as documented by ADR-0013.
- The dashboard can be served by the Compose Nginx container or another static
  host that proxies the versioned API and SSE paths to the Go service.
