# Conversation context

Conversation owns the user-facing Session and Chat turn lifecycle. It combines
retrieval with optional generation, keeps edit/prune and delete operations safe,
and exposes typed contracts to HTTP without knowing transport details.

| Task | Start here | Related |
|---|---|---|
| Start a turn, edit in place, delete history, or replay | `chat/` | `history/`, `retrieval/` |
| Define generation event kinds | `generation/` | Ollama generator, HTTP chat |
| Define persisted Session/Turn values | `history/` | Qdrant history, HTTP contract |

Change an existing child when the concept already exists. Add a new child only
for a separate conversation concept with an independent lifecycle and seam.
Generation ownership stays in `chat/`; storage stays in an Adapter; SSE stays
in `transport/http/chat/`.

## Verification

Run the affected package tests with `go test ./internal/conversation/...` and
include race tests for changes to turn state, replay, or mutation coordination.
