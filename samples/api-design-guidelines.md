---
title: "API Design Guidelines"
description: "Mandatory API design conventions for internal services: data integrity rules, naming, error handling, and versioning for backend HTTP APIs."
aliases: []
tags: [cs, cs/api]
created: "2026-06-13"
---

# API Design Guidelines

This document defines mandatory API design conventions for all internal services. Every endpoint must conform to these rules unless an exemption is approved. Intended for backend engineers building or maintaining HTTP APIs.

## Data Integrity & Types

* Use **Integers** as currency to eliminate floating-point rounding errors. Always pair the value with a Currency Code (ISO 4217).
* Treat null, empty, and default values carefully. In Fintech, `0` is a valid balance, not "missing data."
* If you accept a partial payload (PATCH), ensure your validation logic explicitly rejects _unknown_ fields.

## Performance & Efficiency

* Don't Base64 encode large files. Use `multipart/form-data` for uploads and binary streaming with correct `Content-Type` headers for downloads.
* Apply dual-layer limiting. Use Infrastructure-level limiting (e.g., Nginx, API Gateway) to stop DDoS, and Application-level limiting (e.g., Redis Token Bucket) for business rules per user/tenant.
* Use pagination (cursor-based preferred over offset-based for large datasets[^2]), filtering, and sorting on all collection endpoints.
* All internal APIs must accept and use standard Tracing Headers (e.g., W3C `traceparent` or B3 headers[^6]) to ensure we can debug a request across microservices.
* For batch/bulk operations, clarify whether it's an Atomic Transaction or allows 'Partial Success'. If partial success is allowed, the response structure must explicitly map individual IDs to their success/error status.
* For long running operations do not block the HTTP request. Return `202 Accepted` with a `Location` header pointing to a status polling endpoint, or use Webhooks.

## Reliability & Safety

* Use idempotency for state-changing operations (POST/PATCH)[^1]. Cache the response result (200/422) with a TTL; if a client retries with the same key, return the cached response immediately without re-processing.
* Set endpoint timeouts, implement Circuit Breakers to fail fast when downstream dependencies are unhealthy[^9].
* Redact PII/PCI from logs and traces. In Fintech, logging a raw request body that contains a credit card number or a refresh token can have severe consequences; always apply log sanitization.

## HTTP Semantics & Status Codes

* **Collections:** A search that finds nothing (e.g., `GET /transactions?date=today`) is a success. Return `200 OK` with an empty list `[]`.
* **Resources:** A request for a specific entity that is missing (e.g., `GET /transactions/tx-123`) must return `404 Not Found`.
* **Consistent Errors:** Standardize error responses (e.g., RFC 7807 Problem Details[^5]) across internal APIs so clients can parse `code`, `message`, and `details` uniformly.

## Security

* Authenticate (who are you?) before Authorizing (what can you do?). Enforce Role-Based (RBAC) or Attribute-Based (ABAC) access control at the endpoint level.
* Enforce TLS 1.2+ and use strict CORS policies.

## Lifecycle & Versioning

* Never break a live API. When introducing v2, deploy it alongside v1. Mark v1 as deprecated (via headers), monitor traffic until it hits zero, and then remove.
* Use explicit versioning in the URL (`/v1/`) or Header (`Accept-Version`), each with their own tradeoffs.
* Document the _actual_ behavior, including edge cases and error responses. Use schema-first design (OpenAPI/Swagger)[^3][^4] to ensure implementation matches documentation.

## Request Coalescing

Use request coalescing to deduplicate concurrent identical requests to the same upstream[^8]. When a request arrives for key `X`, check if an upstream call for `X` is already in-flight. If yes, subscribe the caller to the existing result instead of opening a new connection. Go's `golang.org/x/sync/singleflight`[^7] implements this pattern.

## References

[^1]: [Stripe API Reference: Idempotent Requests](https://stripe.com/docs/api/idempotent_requests)
[^2]: [Stripe API Reference: Pagination](https://stripe.com/docs/api/pagination)
[^3]: [Google Cloud API Design Guide](https://cloud.google.com/apis/design)
[^4]: [Microsoft REST API Guidelines](https://github.com/microsoft/api-guidelines)
[^5]: [RFC 7807 Problem Details for HTTP APIs](https://datatracker.ietf.org/doc/html/rfc7807)
[^6]: [W3C Trace Context](https://www.w3.org/TR/trace-context/)
[^7]: [Go singleflight package](https://pkg.go.dev/golang.org/x/sync/singleflight)
[^8]: [Zapier Engineering: Request Coalescing](https://zapier.com/engineering/request-coalescing/)
[^9]: [AWS: Timeouts, Retries, and Backoff with Jitter](https://aws.amazon.com/builders-library/timeouts-retries-and-backoff-with-jitter/)
[^10]: [Stripe: Designing APIs for Humans](https://stripe.com/blog/designing-apis-for-humans)
[^11]: [GitHub API v3: Conditional Requests](https://docs.github.com/en/rest/overview/resources-in-the-rest-api#conditional-requests)
