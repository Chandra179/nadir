# 0015 — In-place chat turn editing

Date: 2026-09-12

## Status

Accepted; supersedes [ADR-0012](0012-turn-copy-and-edit-append-only-re-ask.md)

## Context

The original edit behavior appended a second copy of the question to the
same Session because chat history was append-only. That left the incorrect
question and every later turn visible, which did not match the expected chat
editing behavior.

## Decision

Editing a previous Chat turn keeps the same Session, removes the selected turn
and every later turn, and runs the replacement against the retained prefix.
The edit position is an ordered zero-based sequence boundary. If truncation
fails, Retrieval and generation do not start.

The existing chat request and stream flow are reused. The history Module owns
the truncation operation; the chat Module coordinates truncation, Retrieval,
generation, and persistence. The dashboard removes the rendered tail before
the replacement arrives.

## Consequences

Editing is destructive to the selected tail and does not create hidden
branches. The replacement becomes the next turn in the same Session. A
future undo feature would need a separate retention or revision policy.
