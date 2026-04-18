# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Vendor dependencies
make vendor           # runs: go mod tidy && go mod vendor

# Run HTTP server
go run cmd/http/main.go

# Run gRPC server
go run cmd/grpc/main.go

# Build
go build ./...

# Test
go test ./...
go test ./internal/middleware/...   # test a specific package

# Single test
go test -run TestFunctionName ./internal/middleware/...
```

Config is loaded from `config/config.yaml` (HTTP addr, gRPC addr, timeouts, logger level).

## Architecture

This is a Go skeleton with two separate server binaries (HTTP and gRPC), sharing middleware and config. Dependencies are only the standard library plus `github.com/Chandra179/gosdk` for logging.

**Entry points**:
- `cmd/http/main.go` → `httpserver.Server(cfg)` in `internal/httpserver/server.go`
- `cmd/grpc/main.go` → `grpcserver.Server(cfg)` in `internal/grpcserver/server.go`

**`internal/httpserver/server.go`** wires the HTTP server:
- Creates a `Dependencies` struct (holds the logger)
- Builds a `globalChain` applied to all routes: Recovery → RequestID → Timeout
- Registers routes on `http.ServeMux` with per-route middleware chains on top

**`internal/grpcserver/server.go`** wires the gRPC server:
- Uses `grpc.ChainUnaryInterceptor` with `middleware.RequestIDUnaryInterceptor`
- Register service implementations here via `pb.RegisterXxxServer(srv, &impl{})`

**`internal/middleware/`** contains all middleware:
- `chain.go` — `Chain(handler, ...Middleware)` applies middlewares outermost-first
- `dependencies.go` — `Dependencies` struct; middleware needing shared state (logger) are methods on it
- `request_id.go` — HTTP middleware + gRPC unary interceptor; propagates/generates `X-Request-ID` / `x-request-id`
- `request_validation.go` — generic `Validate[T]` helper using struct tags
- `recovery.go`, `timeout.go` — standard HTTP middleware

**Pattern**: stateless middleware are plain `func(...) Middleware` constructors. Middleware needing the logger are methods on `*Dependencies`.
