# HTTP transport

Maps versioned JSON and SSE requests to bounded-context capabilities and maps
results to HTTP status and response contracts. It owns routes and wire
translation, not indexing, ranking, Chat lifecycle, or provider behavior.

Change here for top-level routes, status mapping, or shared HTTP dependency
wiring. Use child folders for feature-specific mappings. Verify the HTTP unit
and end-to-end tests.
