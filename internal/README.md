# Modules

Each module is a Go package under `modules/<name>/`. Module owns its domain logic, transport, and DI.

## Required files

| File | Purpose |
|------|---------|
| `dependencies.go` | Unexported `dependencies` struct holding the module's wired deps (e.g. `logger`, `store`); exported `DependenciesConfig` struct callers fill in; `NewDependencies(*DependenciesConfig) *dependencies` constructor. Handlers are methods on `*dependencies`. |
| `types.go` | Domain types, structs, constants |

## Optional files

| File | Purpose |
|------|---------|
| `interface.go` | The module's own `Store` interface (only if it needs persistence) |
| `handler.go` | Module entrypoint — HTTP handlers |
| `business_error.go` | Domain sentinels (plain `errors.New(...)`, no non-stdlib imports) |
| `<action>.go` | One file per handler/operation (e.g. `create_order.go`, `get_order.go`) |
