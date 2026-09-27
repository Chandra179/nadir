# HTTP middleware

The candidate HTTP server composes standard `net/http` middleware in `internal/bootstrap/server`. Request ID, timeout, request logging, and panic recovery run around the `internal/edge/http` router.

This package owns process-wide HTTP concerns only. The edge package owns request decoding, validation, route methods, status codes, JSON shapes, and SSE. Core modules do not import either package.

Request logging records method, path, status, duration, and trace IDs without request or response bodies. Timeout middleware exempts source sweeps and SSE streams, which have their own lifecycle.

Check middleware and registration with `go test ./internal/bootstrap/httpmiddleware ./internal/bootstrap/server`.
