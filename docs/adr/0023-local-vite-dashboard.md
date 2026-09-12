# 0023 — Run the dashboard with the local Vite toolchain

## Status

Accepted

## Context

The dashboard is a React + TypeScript + Tailwind application and the intended
development workflow already requires Node.js, npm, and npx. The repository's
single-node deployment does not need a second frontend runtime or an Nginx
proxy container. A frontend Dockerfile duplicated the local build toolchain,
added another image and Compose service, and made the supported workflow more
complex without improving the current backend consistency model.

## Decision

Remove the dashboard Dockerfile, Nginx configuration, and dashboard service from
Compose. Run the dashboard locally with `npm ci` followed by `npm run dev`.
Vite proxies `/api/` and SSE traffic to the Go API on `localhost:8100`.

The backend Compose stack continues to provide the Go API, Qdrant, and the
reranker. A production operator may build the dashboard with `npm run build`
and serve `dist/` from an independently managed static host; that hosting
choice is outside this repository's supported Compose deployment.

The JSON/SSE contract and React migration remain governed by ADR-0021. This
decision changes only the frontend packaging and local deployment path.

## Consequences

- Local development has one clear frontend workflow using the host Node.js
  installation.
- Compose is smaller and no longer builds a frontend image or runs Nginx.
- CI continues to run npm install, typecheck, lint, tests, build, and Playwright
  checks directly in the dashboard package.
- A one-command Compose deployment no longer includes a browser server; users
  start the dashboard separately, or provide their own static host in
  production.
- Cross-platform behaviour is simpler because Docker is still used only for
  backend services and the CPU-safe reranker base stack.
