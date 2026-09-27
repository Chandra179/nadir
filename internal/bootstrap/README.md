# Bootstrap

Bootstrap owns configuration, process lifecycle, composition, logging, observability, middleware, and the process-local gate controller. `runtime/` wires the shared indexing and retrieval graph; `server/` adds the HTTP process and shutdown sequence.

| Task | Start here | Related |
|---|---|---|
| YAML or environment mapping | `configuration/` | `internal/bootstrap/configuration/config.yaml` |
| Qdrant, embedding, indexing, and retrieval graph | `runtime/` | `internal/providers/`, `internal/core/` |
| Indexing/destructive gates, reranker gate, background jobs | `gates/` | runtime, providers |
| HTTP lifecycle and shutdown | `server/` | edge, conversation |
| Request IDs, deadlines, recovery, logging | `httpmiddleware/` | `internal/edge/http/` |
| Optional loopback profiling | `profiling/` | configuration |

Business rules belong in Documents, Retrieval, or Conversation. Keep providers behind narrow core interfaces.

Run `go test -race ./internal/bootstrap/...`, `go vet ./internal/bootstrap/...`, and `go build ./cmd/api` after lifecycle changes.
