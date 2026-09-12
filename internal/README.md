# Modules

Private Go code lives under `internal/` and is organized by bounded context.
Knowledge, Retrieval, Conversation, and Evaluation own domain behavior;
Transport maps HTTP; Adapters integrate external systems; Platform owns
cross-cutting runtime concerns.

The executable applications under `cmd/` are intentionally thin entrypoints:
`cmd/api` starts the HTTP application and `cmd/evaluator` runs quality
measurement. They do not contain domain logic. The React application under
`web/dashboard` is a separate client organized by feature.

## Required files

| File | Purpose |
|------|---------|
| `dependencies.go` | Exported `DependenciesConfig` struct callers fill in; unexported `dependencies` struct holding the Module's wired deps; `NewDependencies(DependenciesConfig)` constructor. Callers pass provider-owned Interfaces and concrete Adapters are assembled only by the lifecycle composition code. |
| `types.go` | Domain types, structs, constants |

## Optional files

| File | Purpose |
|------|---------|
| `interface.go` | The module's interfaces together: the provider-owned `Service` interface (what the module provides to real sibling consumers) and the package-private `store` interface (what it requires), each with its `var _ X = ...` compile-time assertion |
| `handler.go` | Module entrypoint — HTTP handlers |
| `business_error.go` | Domain sentinels (plain `errors.New(...)`, no non-stdlib imports) |
| `constant.go` | Unexported package constants |
| `<action>.go` | One file per handler/operation (e.g. `create_example.go`); holds the `*dependencies`/store method implementations that would otherwise live in `service.go`/`store.go` |

There is no separate `service.go`/`store.go` — the `Service` and `store`
interfaces live in `interface.go`, and implementations are split across
per-action files like `create_example.go`. `Service` is exported only when a
real sibling consumer needs it. `store` remains package-private because it is
an implementation port. Mockery generates its test mock under `mocks/`; its
method names are exported when an external generated mock must satisfy the
private interface.

## Cross-module communication

Modules call each other in-process, through an interface — never by importing
and holding a sibling module's concrete `*dependencies` type directly.

- **`Service`** (in `interface.go`) is what a module *provides* to other
  modules. It's the module's own public contract:

```go
// internal/knowledge/interface.go — Knowledge provides this to callers
type Service interface {
    CreateExample(ctx context.Context, name string) (*Example, error)
}

var _ Service = (*dependencies)(nil)
```

A module that consumes a sibling depends on that sibling's `Service`
interface, wired in via its own `DependenciesConfig`.

`internal/platform/lifecycle/server.go` constructs the concrete dependencies,
passes infrastructure into constructors, and passes the returned value to
other contexts as the provider's Interface type. HTTP handlers remain in
`internal/transport/http` and only adapt requests, responses, and SSE.

Only add a `Service` interface for a real cross-module contract. Do not impose a
universal one-public-interface rule: expose small provider-owned contracts when
there are multiple meaningful consumers, and keep internal ports private.

For tests, use the package-private `newDependencies` helper pattern (exposed
to external tests only through `export_test.go`) to inject the Mockery-generated
`store` mock. External sibling modules should mock the
exported `Service` contract, not the provider's storage port.
