# Platform

Platform owns process-wide runtime concerns: configuration, lifecycle and
composition, HTTP middleware, logging, and lightweight observability. The
`runtime/` child assembles the shared retrieval/indexing graph; `server/`
assembles the HTTP process around it.

| Task | Start here | Related |
|---|---|---|
| Config key, env override, validation, or default | `configuration/` | `config/config.yaml`, deployment |
| Shared Qdrant/embed/index/search graph | `runtime/` | `adapters/`, bounded contexts |
| HTTP startup, shutdown, and dependency injection | `server/` | `runtime/`, chat, history, transport |
| Request IDs, timeout, recovery, request logs | `httpmiddleware/` | `transport/http/` |
| Logger construction or stage labels | `logging/`, `observability/` | affected operation |

Do not put business policy here. If a rule is specific to Documents,
Retrieval, or Conversation, keep it in that context and pass its dependencies
from server. A new cross-cutting concern belongs in a new child only when it
has a process-wide contract and more than one real consumer.

## Verification

Run `go test -race ./internal/platform/...`, `go vet ./internal/platform/...`,
and `go build ./cmd/api` after composition or lifecycle changes.
