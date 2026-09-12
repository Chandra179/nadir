# 0017 — Chat history mutation ownership

Date: 2026-09-12

## Status

Accepted

## Context

Chat generation and ordinary history persistence are intentionally detached
from the HTTP request. That keeps a slow browser or history store from
blocking the live response, but it also means a completed generation can try
to append after an in-place edit or a destructive session deletion.

## Decision

The chat Module owns the lifecycle seam for history mutations:

- each session turn captures a session and global mutation revision;
- in-place editing serializes the tail prune, advances the session revision,
  and cancels active generation for that session;
- single-session deletion advances that session revision and cancels active
  generation before deleting persisted records;
- delete-all advances the global revision, cancels every active generation,
  and then deletes persisted records; and
- detached appends are serialized with destructive operations and are
  discarded when their captured revision is stale.

The HTTP history transport sends destructive operations through the chat
Module, while the history Module remains responsible for Qdrant persistence.

## Consequences

Pruned or deleted Chat turns cannot be recreated by an older generation that
finishes later. A concurrent turn can still complete its in-memory Retrieval
or generation work, but its stale result is not persisted and the caller can
retry against the current Session. The current revision registry is
process-local, matching the single-node event-log decision in ADR-0013; a
shared history/event backend will need a distributed version or fencing token
when Nadir scales horizontally.
