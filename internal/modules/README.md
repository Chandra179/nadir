# Service

internal module that will not exported or used outside of this package, directory structure:
```
think in architectural characteristics when it become a module
- module name
  - init.go: initialize order module
  - dependencies.go: logger abstraction, client abstraction
  - types.go: input and output for create order could be struct, constant, etc..
  - operation name: create order, get order
```