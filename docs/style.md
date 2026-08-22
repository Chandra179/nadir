# Go Style Guide

Baseline: [uber-go/guide](https://github.com/uber-go/guide). For anything not
covered below, that's the source of truth. See also: [`errors.md`](errors.md) for wrapping conventions
[`logging.md`](logging.md) for logging conventions.

---

- Verify compliance at compile time: `var _ Interface = (*Type)(nil)` next to the type definition  its for interface implementation
- Always pass interface as param not pass the concrete implementation (its not mockable) and violate dependency inversion principle
- Avoid mutable package-level globals. Pass dependencies in explicitly.
- if function have more or less than 5 param make it into struct