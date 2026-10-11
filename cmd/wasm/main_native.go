//go:build !(js && wasm)

// Command wasm is the browser runtime entrypoint (GOOS=js GOARCH=wasm). It
// assembles the same indexing and retrieval graph as cmd/api over in-memory
// storage and host-supplied model functions. See main.go; build it with
// `make wasm-build`.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "cmd/wasm only runs as WebAssembly: use `make wasm-build`")
	os.Exit(2)
}
