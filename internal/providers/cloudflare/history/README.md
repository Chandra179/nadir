# Cloudflare D1 history Adapter

Persists Conversation sessions and turns in two D1 tables, `chat_sessions` and
`chat_turns`, created on startup with `CREATE TABLE IF NOT EXISTS`. It owns the
SQL, row encoding and D1 errors; Chat owns mutation ordering, revision checks
and deletion policy.

- A turn's sequence is the session's turn count at insert time, and
  `(session_id, sequence)` is the primary key, so a duplicate fails instead of
  corrupting order.
- A session's `turn_count` is derived from its turns, so it cannot drift.
- Multi-statement writes are sent as one D1 batch. Truncate and append also take
  a process-local mutex, as the Qdrant Adapter did.
- Turns are read in `history.turn_page_size` pages by sequence.
- Evidence snapshots (results, citations, subject, attached files) are stored as
  JSON text, exactly as the Qdrant Adapter stored them.

Tests run this SQL on SQLite behind a fake D1 REST server. They do not prove
D1's own behavior (batch atomicity over REST, row-size limits); see ADR 0041.

Change here for the history storage schema. Verify with
`go test ./internal/providers/cloudflare/history`.
