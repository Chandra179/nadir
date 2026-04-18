# Skeleton: Evolutionary Architecture (Golang)

This repo is an example for golang template project, modules name, functionality is just an example

## Architectural Definitions
* **Component (The App):** This entire repository is a single Component. It is an independently deployable unit that provides a set of related business capabilities.
* **Modules (Internal Logic):** Located in `internal/modules/`, these are logical wrappers (Go packages) used to maintain high **Functional Cohesion**. 

## Why this Structure?
1.  **Modularity:** Logic is partitioned by domain (`order`, `calc`) rather than technical layers.
2.  **Fitness Functions:** This structure allows you to write tests (e.g., using `ArchGuard` or `go-cyclomatic`) to ensure the `calc` module doesn't accidentally start importing `httpserver` logic.
3.  **Evolutionary Path:** If the `calc` module's architecture characteristics change (e.g., it needs massive scalability), it is decoupled enough to be extracted into a separate **Architecture Quantum**.

## Project Structure

```
.
├── cmd/                          # Entry points
│   ├── http/main.go             # HTTP server binary
│   └── grpc/main.go             # gRPC server binary
├── internal/
│   ├── httpserver/              # HTTP server setup
│   │   └── server.go
│   ├── grpcserver/              # gRPC server setup
│   │   └── server.go
│   ├── middleware/              # Shared middleware
│   │   ├── chain.go             # Middleware chaining
│   │   ├── dependencies.go      # Shared dependencies
│   │   ├── request_id.go        # Request ID propagation
│   │   ├── request_validation.go # Request validation helper
│   │   ├── recovery.go          # Panic recovery
│   │   ├── timeout.go           # Request timeout
│   │   └── README.md
│   └── modules/                 # Business logic (components)
│       ├── order/               # Order module
│       │   ├── init.go
│       │   ├── types.go
│       │   ├── create_order.go
│       │   ├── get_order.go
│       │   └── dependencies.go
│       ├── calc/                # Calc module
│       │   └── dependencies.go
│       └── README.md
├── config/
│   ├── config.yaml             # Configuration (addresses, timeouts, logger level)
│   └── config.go
├── Makefile                     # Build commands
├── go.mod & go.sum             # Dependency management
├── CLAUDE.md                    # Claude Code instructions
└── README.md                    # This file
```
