# Transport

Transport contains external delivery mechanisms. The current child is HTTP;
its job is to translate wire requests and responses into bounded-context calls.
Transport owns protocol details, not Document, Retrieval, or Conversation
business rules.

## Task routing

| Task | Start here |
|---|---|
| Add/change versioned HTTP endpoint | `http/` |
| Add/change chat JSON or SSE mapping | `http/chat/` |
| Add/change session history endpoints | `http/history/` |
| Share public HTTP response shapes | `http/contract/` |

Add another transport child only for a real protocol such as gRPC or a worker
consumer with its own lifecycle. Do not duplicate domain logic in a second
transport. Keep composition in `platform/lifecycle/`.

## Verification

Run the affected HTTP tests and `go test -race ./internal/transport/...` for
transport changes. Include the end-to-end HTTP workflow for route changes.
