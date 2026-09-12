# Chat stream contract

`GET /api/v1/turns/{turn_id}/events` is a Server-Sent Events stream. The Go
API owns generation and persistence; the browser is only a subscriber.

Every event has an increasing `id` cursor. A reconnecting client sends that
cursor in `Last-Event-ID` and receives retained events after it.

| Event | Data | Meaning |
|---|---|---|
| `token` | plain text | Append the text to the answer. It is not HTML. |
| `done` | `1` | Generation reached a terminal state; close the stream and refresh the session list. |
| `generror` | plain text | Generation failed; keep any already received answer text. |
| `resync` | message | The cursor fell outside bounded retention; reload the persisted session. |

The event log is bounded and ordered per process. It is not a distributed
broker. Horizontal deployment requires a shared event backend and subscriber
routing, as described in `docs/SCALING.md`.
