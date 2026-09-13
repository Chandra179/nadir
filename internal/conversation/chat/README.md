# Chat use-case

Owns sessions and turn lifecycle: edit/prune, retrieval orchestration,
generation supervision, cancellation, event replay, bounded retention, and
history mutation ordering.

Change here for user-visible Chat behavior or lifecycle invariants. Use
`retrieval/` for ranking and `transport/http/chat/` for wire mapping. Verify
with `go test -race ./internal/conversation/chat`.

`interface.go` contains the public `Chat` contract; the history persistence
seam is private in `private_interfaces.go`.
