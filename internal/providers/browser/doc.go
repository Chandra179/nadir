// Package browser holds the Go side of the JavaScript bridge used by the
// browser (js/wasm) runtime: promise interop and adapters that delegate model
// work to functions supplied by the host page. Everything except this file is
// built only for GOOS=js GOARCH=wasm.
package browser
