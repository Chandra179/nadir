# 12. Turn copy and edit: append-only re-ask

Date: 2026-09-06

## Status

Accepted

## Context

Chat turns were static: neither the question nor the answer could be copied
out of the dashboard, and a mistyped question could only be re-sent from the
composer by retyping it. ChatUIs (ChatGPT et al.) expose hover actions —
copy on every message, edit on the user's question — and reserve space
between the question bubble and the answer for those actions, so the layout
does not jump when they appear.

Nadir's history model is append-only: turns are written to the session at
their terminal state, and there is no branch, rewind, or delete-turn API
(sessions are deleted whole). Making edit feel like ChatGPT (truncate the
conversation and regenerate in place) would require turn-level deletion and
branching in `internal/history` — new domain surface for a UI affordance.

## Decision

Each turn's question block gets hover-revealed copy + edit actions, and the
answer block gets a copy action; the action row always occupies its space
(between question and answer, right-aligned under the bubble) and is only
made visible by hover — fully visible on touch devices, which have no hover.

- **Copy** reads the rendered text from the DOM (`innerText` of the marked
  element) and writes it through `navigator.clipboard`; the icon swaps to a
  checkmark briefly. Works identically on live and replayed turns because
  both render the same `turn` template.
- **Edit** opens an inline editor (the question bubble is replaced by a
  textarea with Cancel/Send). Sending posts the same `POST /retrieval/search`
  as the composer, carrying the turn's original parameters (`top_k`,
  `generate`, attachment names) plus the turn's `SessionID` (added to
  `TurnView` for this), so the edited question is re-asked as a **new turn
  appended to the same session**. Nothing is deleted, rewritten, or branched;
  the original turn stays in place.

The editor is a second htmx form over the same endpoint, reusing the
composer's optimistic placeholder and busy/stop handling (shared
`nadirAppendPendingTurn`); no new backend route exists.

## Consequences

Edit semantics differ from ChatGPT: the edited exchange appears below the
original instead of replacing it, because history is append-only. If
in-place editing is ever wanted, it costs turn-level deletion + branching in
the history domain first.

Copying reads rendered text, so it always matches what the user sees —
including a partially streamed answer — with no extra server round-trip.

The hover reveal is plain CSS scoped to the question/answer block, not a
Tailwind utility chain, so it survives Tailwind CDN changes and works with
touch fallback via `@media (hover: none)`.
