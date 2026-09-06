# 0007 — Dashboard streams via a plain EventSource; htmx's sse extension is removed

- **Status:** Accepted
- **Date:** 2026-09-06
- **Deciders:** Chandra, ZCode session

## Context

The dashboard's streaming answers were wired with htmx's sse extension
(`unpkg.com/htmx.org@2.0.4/dist/ext/sse.js`, `hx-ext="sse"` +
`sse-connect`/`sse-swap`). The stream connected (devtools showed `GET
/retrieval/turns/:id/events → 200`) but the UI never rendered a token, and
the terminal event never ran.

Root cause, verified statically: the `dist/ext/sse.js` shipped in the htmx.org
npm package is stale htmx-1-era code (its header even warns about it) and its
swap path calls `api.selectAndSwap(...)` — which has **zero occurrences in
htmx 2.0.4 core**. The first token event therefore threw `TypeError:
api.selectAndSwap is not a function`, before any DOM update or event
dispatch. Pinning a hypothetical fixed extension version on a CDN was not
attractive either — the app only needs a fraction of what the extension does.

## Decision

Drop the extension; wire streaming directly:

- The turn fragment marks the answer area with `data-stream-url` and
  `data-turn-id`; a document-level `htmx:afterSwap` listener starts a plain
  `EventSource` per streaming turn appended to the reader.
- Tokens append via `insertAdjacentText` — the SSE payload stays raw text
  (no HTML escaping on either side; `textContent` semantics make it inert).
- Terminal events (`done`, `generror`) close the source, stop the cursor,
  refresh the sidebar (persistence happens at stream end, not at POST
  response), and restore the composer.
- The composer's send button doubles as the **stop button** while a turn is
  retrieving or streaming: clicking it prevents the submit and POSTs
  `/retrieval/turns/:id/cancel` (ADR 0006). A busy counter spans retrieval
  plus stream, so the icon state always reflects real work.
- A fatal transport error (source closed) ends the stream client-side,
  keeping whatever text arrived.

## Consequences

- No third-party extension to trust or pin; ~40 lines of app-owned JS.
- Reconnect/resume still works via `Last-Event-ID` replay plus the server's
  204-for-exhausted-streams rule.
- Answers render as plain text only (no markdown/HTML), matching history
  replay.
