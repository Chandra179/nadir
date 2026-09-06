# 0006 — Chat turns stream over a domain-owned event log (SSE)

- **Status:** Accepted
- **Date:** 2026-09-06
- **Deciders:** Chandra, ZCode session

## Context

Chat was synchronous HTTP: `POST /retrieval/search` ran rewrite → retrieve →
prompt build → buffered the entire Ollama token stream (`io.ReadAll`) →
rendered one HTML fragment. Consequences:

- A long answer held the request for the whole generation; the UI showed
  nothing meanwhile.
- The Ollama stream was bound to the POST request context, so the response
  returning killed generation mid-flight (observed as 404s on the follow-up
  answer endpoint).
- Server timeouts (middleware 30s, `http.WriteTimeout`) fought long
  generations; `write_timeout` had to become `0`.
- There was no way to watch progress or cancel.

A first implementation handed the open Ollama stream (`io.ReadCloser`) to an
api-layer "pending registry" keyed by a random id. That kept generation
ownership in the transport: single consumer, no replay, and the answer was
lost if the browser never connected.

## Decision

Generation is owned by the chat domain; transports only observe.

- **Typed events.** `generator.Generate(ctx, prompt)` dials Ollama
  synchronously (start failures are deterministic errors) and feeds a sealed
  event stream (`TokenEvent` / `ErrorEvent` / `DoneEvent`) from its own
  goroutine. Prompt building moved out of the generator into the chat
  use-case (`internal/chat/prompt.go`) — assembling context is use-case
  logic, not a client concern.
- **Supervisor.** `chat.StartTurn` runs rewrite → retrieve → prompt build,
  dials the generator on a detached context (`context.WithoutCancel`), and
  supervises the stream on its own goroutine. The POST only renders the
  trace plus a stream URL. The turn is persisted by the supervisor at its
  terminal state (answer, error, or cancellation), never by a handler.
- **Event log.** A per-turn broker keeps a `[]TurnEvent` log with monotonic
  cursors (`Seq`). Any number of subscribers attach at any cursor and are
  replayed first; the log outlives individual HTTP connections. Retention is
  capped (64 finished turns) to bound memory.
- **SSE adapter.** `GET /retrieval/turns/:id/events` is a thin subscriber:
  `id:`/`event:`/`data:` framing, payload raw (the browser inserts it with
  `textContent`), `Last-Event-ID` resumes the cursor, and an exhausted
  finished stream answers `204` so the browser stops reconnecting.
- **Cancellation.** `POST /retrieval/turns/:id/cancel` → `chat.CancelTurn`
  aborts the detached context; the supervisor keeps the answer generated so
  far, publishes the terminal event, and persists the partial turn.
- **Timeouts.** `http.write_timeout: 0s` (streamed answers outlive any fixed
  write deadline; generation stays bounded by the generator's own client
  timeout) and the middleware deadline excludes `/retrieval/turns/`.

Rejected: WebSocket (heavier, and replay/cursor semantics would be
hand-rolled anyway); the pending-registry stream handoff (first
implementation, reasons above); keeping buffered HTTP.

## Consequences

- A turn completes and persists even with zero subscribers (browser closed
  instantly) — generation is not tied to any connection.
- A page reload mid-generation loses the live view; the turn appears only
  after the supervisor persists it.
- `/retrieval/search` now responds as soon as retrieval finishes; latency
  budget of the POST no longer includes generation.
- `generator.max_context_tokens` is a chat concern (prompt budget), not a
  generator one.
