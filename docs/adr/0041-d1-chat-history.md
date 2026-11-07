# 0041 — Chat history in Cloudflare D1

- **Status:** Accepted
- **Date:** 2026-11-07
- **Deciders:** Chandra

## Context

Chat sessions and turns lived in a Qdrant collection. History is never searched
by similarity: every point carries a constant placeholder vector and is only
listed, filtered and ordered by payload. Qdrant therefore stores relational data
with a vector it does not need, and history shares the collection dimension
with documents for no reader. D1 is Cloudflare's SQLite database and fits
sessions-with-turns directly.

## Decision

- `history.provider` selects `qdrant` (default) or `d1`. The D1 Adapter is
  `internal/providers/cloudflare/history` over a small REST client,
  `internal/providers/cloudflare/d1`, on the shared `cfapi` transport.
- Credentials come from the shared `cloudflare` section plus
  `cloudflare.d1_database_id` (`CLOUDFLARE_D1_DATABASE_ID`). The token needs D1
  edit access. The database must already exist; the Adapter creates its two
  tables on startup.
- Schema: `chat_sessions(id, title, created_at, updated_at)` and
  `chat_turns(session_id, sequence, ...)` with primary key
  `(session_id, sequence)`. Turn counts are derived with `COUNT(*)`, not stored.
  Evidence snapshots stay JSON text so the public turn shape is unchanged.
- The D1 client retries only 429, never 503: a rate-limited request was not
  executed, but replaying a possibly-executed write could duplicate a turn.
- Switching provider starts with empty history. There is no migration; both
  stores can be kept side by side and the old one read by switching back.
- Qdrant stays the default until D1 history has run against a real database.

## Consequences

- Every history read and write is a network round trip to Cloudflare, including
  loading prior turns for follow-up rewriting. Conversation text and retrieved
  evidence leave the machine.
- D1 documents 100 bound parameters per statement (a turn insert uses 22), a
  100 KB statement length and a 30 second query limit. Turns carrying very large
  prompts or evidence could approach D1's per-row size limit; that limit was not
  checked here.
- Not verified against live D1: the sandbox cannot reach Cloudflare. The REST
  request and result envelope, and whether a REST `batch` is applied
  transactionally, are taken from third-party mirrors of the API reference.
  Writes are written so a partial batch cannot corrupt state: counts are derived
  and the sequence is guarded by the primary key.
- Tests execute the Adapter's SQL on SQLite (a test-only `modernc.org/sqlite`
  dependency) behind a fake D1 server; that proves the SQL and mapping, not D1.
