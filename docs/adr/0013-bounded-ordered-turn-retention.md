# 0013 — Bounded ordered turn retention

## Context

The chat broker must keep completed turn streams available long enough for SSE
reconnects, but an in-process map and replay log cannot grow with traffic or
client disconnects. Random map iteration also made eviction nondeterministic.

## Decision

Keep the current in-process broker for single-node deployments, with:

- insertion-ordered retention of at most 64 turn streams;
- eviction of finished streams first, with a ten-minute finished-stream TTL;
- a bounded replay log of at most 4096 events and 1 MiB of event text;
- explicit resync notification when a reconnect cursor is older than the
  retained window; and
- rejection of new generations when all retained streams are active.

Filtered retrieval bypasses the semantic cache, and cache entries are
versioned against the embedding configuration. If horizontal scaling becomes
a requirement, replace the broker implementation behind the chat event-log
boundary with a shared backend such as Redis Streams; that is intentionally
not a required local dependency.

## Consequences

Memory use and eviction behavior are predictable on one node. A client that
falls behind receives a reconnect/resync signal instead of silently losing
tokens. Multi-node streaming remains a future deployment choice and requires
shared event-log storage plus subscriber routing.
