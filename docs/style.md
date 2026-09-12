# Go Style Guide

The official Go baseline is [Effective Go](https://go.dev/doc/effective_go),
[Go Code Review Comments](https://go.dev/wiki/CodeReviewComments), and the
[standard-library documentation](https://pkg.go.dev/std). The
[uber-go/guide](https://github.com/uber-go/guide) adds practical guidance for
production code and is this repository's default where the official guidance
does not make a choice. See also: [`errors.md`](errors.md) for wrapping
conventions and [`logging.md`](logging.md) for logging conventions.

---

- Verify interface compliance at compile time with
  `var _ Interface = (*Type)(nil)` next to the implementation.
- Accept interfaces at dependency seams when the caller needs substitution;
  do not turn every concrete value into an interface speculatively. Return
  concrete types from constructors where practical.
- Avoid mutable package-level globals. Pass dependencies explicitly.
- If a function has more than five related parameters, group them into a
  configuration or request struct.
- Keep module boundaries aligned with domain ownership, invariants, data
  ownership, and change cadence—not with arbitrary file counts. See
  [`../internal/README.md`](../internal/README.md) for the module contract.
- Use Mockery-generated mocks for interfaces at unit-test seams. Keep generated
  mocks under `mocks/`, out of production code, and configure Mockery once in
  `.mockery.yaml`; do not hand-write a new fake for every test file. A small
  hand-written stub remains acceptable when it is clearer for a value object or
  a one-line behavior, but it must not duplicate a reusable Mockery seam.
