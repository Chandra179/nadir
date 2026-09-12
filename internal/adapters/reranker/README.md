# Reranker Adapter

Calls the cross-encoder sidecar to score retrieval candidates and returns them
in descending score order. It owns the sidecar HTTP contract, concurrency
bound, timeout/cancellation, and response-shape validation.

## Change here when

- The sidecar endpoint, request/response payload, device/backend contract, or
  concurrency limit changes.
- A reranker provider is replaced or added.

Change `retrieval/search/` for candidate multiplication, top-k selection,
failure degradation, or when reranking is enabled. Change `services/reranker/`
for model loading and inference implementation. Configuration changes belong in
`platform/configuration/` and deployment files.

## Verification

Run `go test ./internal/adapters/reranker` and cover score count mismatch,
non-success responses, empty candidates, and cancellation.
