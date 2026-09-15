# HTTP contracts

`openapi.yaml` is the canonical versioned wire contract for the API. The
dashboard mirror at `web/dashboard/src/lib/api-contract.ts` is checked against
its component schemas by `go run ./cmd/contractcheck` and in CI.

Change both the OpenAPI schema and the transport mapping when a public JSON
field changes. Run the contract check and dashboard typecheck before merging.
